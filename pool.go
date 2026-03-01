package browserpm

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

// pageState represents the lifecycle state of a pooled page.
type pageState int32

const (
	pageIdle    pageState = iota // available for operations
	pageClosing                  // TTL expired; draining active ops
	pageClosed                   // closed and awaiting removal
)

// poolPage wraps a playwright.Page with pool metadata.
// Fields accessed on the hot path use atomic operations.
type poolPage struct {
	page      playwright.Page
	id        string
	state     atomic.Int32 // pageState
	createdAt time.Time
	lastUsed  atomic.Int64 // UnixNano
	activeOps atomic.Int64
	useCount  atomic.Int64
}

func (pp *poolPage) getState() pageState  { return pageState(pp.state.Load()) }
func (pp *poolPage) setState(s pageState) { pp.state.Store(int32(s)) }

func (pp *poolPage) isAvailable(now time.Time, ttl, grace time.Duration) bool {
	if pp.getState() != pageIdle {
		return false
	}
	if ttl > 0 && grace > 0 && now.Sub(pp.createdAt) > ttl-grace {
		return false
	}
	return true
}


// PagePool manages a set of shared pages with scheduling, health checking,
// and TTL-based recycling. The acquire path is optimised for minimal lock
// contention: a read-lock protects the page slice while atomic counters
// handle per-page bookkeeping.
type PagePool struct {
	pages     []*poolPage
	mu        sync.RWMutex
	scheduler Scheduler
	config    PoolConfig
	provider  PageProvider
	log       Logger

	createPage func() (playwright.Page, error)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newPagePool(cfg PoolConfig, provider PageProvider, log Logger, createPage func() (playwright.Page, error)) *PagePool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &PagePool{
		scheduler:  NewScheduler(cfg.ScheduleStrategy),
		config:     cfg,
		provider:   provider,
		log:        log,
		createPage: createPage,
		ctx:        ctx,
		cancel:     cancel,
	}
	return p
}

// Start pre-warms pages and launches background goroutines.
func (p *PagePool) Start() error {
	if err := p.warmUp(p.config.MinPages); err != nil {
		return err
	}
	p.startHealthChecker()
	p.startReaper()
	return nil
}

// warmUp creates pages up to count.
func (p *PagePool) warmUp(count int) error {
	for i := 0; i < count; i++ {
		if err := p.addPage(); err != nil {
			return WrapError(err, ErrInternal, "pool warm-up failed")
		}
	}
	return nil
}

func (p *PagePool) addPage() error {
	pp, err := p.createAndInitPage()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.pages = append(p.pages, pp)
	p.mu.Unlock()
	p.log.Debug("pool page added", String("page_id", pp.id), Int("pool_size", p.Size()))
	return nil
}

func (p *PagePool) createAndInitPage() (*poolPage, error) {
	raw, err := p.createPage()
	if err != nil {
		return nil, WrapError(err, ErrInternal, "failed to create page")
	}

	pp := &poolPage{
		page:      raw,
		id:        generateID(),
		createdAt: time.Now(),
	}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	initCtx, cancel := context.WithTimeout(p.ctx, p.config.InitTimeout)
	defer cancel()
	if err := p.provider.Init(initCtx, raw); err != nil {
		raw.Close()
		return nil, WrapError(err, ErrInternal, "page init failed")
	}
	return pp, nil
}

// Acquire picks an available page via the scheduler. If no pages are idle
// and the pool hasn't reached MaxPages, a new page is created on the fly.
// The caller MUST call Release when done.
//
// Hot path: the common case uses only a read-lock and atomic ops.
func (p *PagePool) Acquire(ctx context.Context) (*poolPage, error) {
	pp, totalPages := p.tryAcquire()
	if pp != nil {
		return pp, nil
	}

	// No idle page — try to grow the pool.
	if totalPages < p.config.MaxPages {
		newPP, err := p.createAndInitPage()
		if err != nil {
			return nil, err
		}
		newPP.activeOps.Add(1)
		newPP.lastUsed.Store(time.Now().UnixNano())
		p.mu.Lock()
		p.pages = append(p.pages, newPP)
		p.mu.Unlock()
		p.log.Debug("pool grew on demand", String("page_id", newPP.id), Int("pool_size", p.Size()))
		return newPP, nil
	}

	// Pool exhausted — spin briefly for a page to become available.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(p.config.OperationTimeout)
	}

	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, WrapError(ctx.Err(), ErrTimeout, "acquire page timed out")
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, NewError(ErrPoolExhausted, "all pages busy and pool at max capacity")
			}
			if pp, _ = p.tryAcquire(); pp != nil {
				return pp, nil
			}
		}
	}
}

// tryAcquire is the fast-path: read-lock + scheduler select + atomic bump.
// Returns (nil, totalPages) when nothing is available.
func (p *PagePool) tryAcquire() (*poolPage, int) {
	now := time.Now()
	p.mu.RLock()
	pp := p.selectAvailable(now)
	n := len(p.pages)
	p.mu.RUnlock()
	if pp != nil {
		pp.activeOps.Add(1)
		pp.lastUsed.Store(now.UnixNano())
		pp.useCount.Add(1)
	}
	return pp, n
}

// selectAvailable filters and selects inline without allocating a slice
// when the pool is healthy (common case). Caller must hold at least a
// read lock.
func (p *PagePool) selectAvailable(now time.Time) *poolPage {
	// Fast path for common case: all pages idle and within TTL.
	allAvailable := true
	for _, pp := range p.pages {
		if !pp.isAvailable(now, p.config.TTL, p.config.GracePeriod) {
			allAvailable = false
			break
		}
	}
	if allAvailable && len(p.pages) > 0 {
		return p.scheduler.Select(p.pages)
	}

	// Slow path: build filtered slice.
	filtered := make([]*poolPage, 0, len(p.pages))
	for _, pp := range p.pages {
		if pp.isAvailable(now, p.config.TTL, p.config.GracePeriod) {
			filtered = append(filtered, pp)
		}
	}
	return p.scheduler.Select(filtered)
}

// Release returns a page to the pool after use.
func (p *PagePool) Release(pp *poolPage) {
	pp.activeOps.Add(-1)
}


// Size returns the total number of pages in the pool (all states).
func (p *PagePool) Size() int {
	p.mu.RLock()
	n := len(p.pages)
	p.mu.RUnlock()
	return n
}

// ActiveOps returns the sum of active operations across all pages.
func (p *PagePool) ActiveOps() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var total int64
	for _, pp := range p.pages {
		total += pp.activeOps.Load()
	}
	return total
}

// removePage removes a poolPage from the slice and closes the underlying
// playwright page. Caller must hold the write lock.
func (p *PagePool) removePageLocked(target *poolPage) {
	for i, pp := range p.pages {
		if pp == target {
			p.pages = append(p.pages[:i], p.pages[i+1:]...)
			break
		}
	}
	target.setState(pageClosed)
	go func() {
		if err := target.page.Close(); err != nil {
			p.log.Debug("page close error", String("page_id", target.id), Err(err))
		}
	}()
}

// replacePage closes a dead page and creates a fresh replacement.
func (p *PagePool) replacePage(target *poolPage) {
	p.mu.Lock()
	p.removePageLocked(target)
	p.mu.Unlock()

	if pp, err := p.createAndInitPage(); err == nil {
		p.mu.Lock()
		p.pages = append(p.pages, pp)
		p.mu.Unlock()
		p.log.Info("page replaced", String("old_id", target.id), String("new_id", pp.id))
	} else {
		p.log.Error("page replacement failed", err, String("old_id", target.id))
	}
}

// Close shuts down background goroutines and closes all pages.
func (p *PagePool) Close() error {
	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pp := range p.pages {
		pp.setState(pageClosed)
		pp.page.Close()
	}
	p.pages = nil
	return nil
}

// Drain waits for all active operations to complete, with a timeout.
func (p *PagePool) Drain(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.ActiveOps() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// RebuildAll closes every page and re-creates MinPages fresh ones.
func (p *PagePool) RebuildAll() error {
	p.mu.Lock()
	old := p.pages
	p.pages = nil
	p.mu.Unlock()

	for _, pp := range old {
		pp.setState(pageClosed)
		pp.page.Close()
	}

	p.scheduler.Reset()
	return p.warmUp(p.config.MinPages)
}
