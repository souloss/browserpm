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

	// releaseCond is broadcast whenever a page is released or active ops
	// reach zero, waking Acquire/Drain waiters instead of busy-polling.
	waitMu      sync.Mutex
	releaseCond *sync.Cond
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
	p.releaseCond = sync.NewCond(&p.waitMu)
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

	// Pool exhausted — wait for a Release signal instead of busy-polling.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(p.config.OperationTimeout)
	}

	// Use a timer to enforce the deadline and a goroutine to wake us on
	// context cancellation, since sync.Cond has no native select support.
	timer := time.AfterFunc(time.Until(deadline), func() {
		p.releaseCond.Broadcast()
	})
	defer timer.Stop()

	// Also wake on context cancellation.
	stop := context.AfterFunc(ctx, func() {
		p.releaseCond.Broadcast()
	})
	defer stop()

	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	for {
		if pp, _ = p.tryAcquire(); pp != nil {
			return pp, nil
		}
		if ctx.Err() != nil {
			return nil, WrapError(ctx.Err(), ErrTimeout, "acquire page timed out")
		}
		if time.Now().After(deadline) {
			return nil, NewError(ErrPoolExhausted, "all pages busy and pool at max capacity")
		}
		p.releaseCond.Wait() // blocks until Release broadcasts
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

// Release returns a page to the pool after use and wakes any waiters.
func (p *PagePool) Release(pp *poolPage) {
	pp.activeOps.Add(-1)
	p.releaseCond.Broadcast()
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
	return p.activeOpsLocked()
}

// activeOpsLocked returns the sum of active operations. Caller must hold at
// least a read lock on p.mu.
func (p *PagePool) activeOpsLocked() int64 {
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
// It uses the releaseCond signal instead of busy-polling.
func (p *PagePool) Drain(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		p.releaseCond.Broadcast()
	})
	defer timer.Stop()

	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	for {
		if p.ActiveOps() == 0 {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		p.releaseCond.Wait()
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

// PoolStats contains runtime statistics for a page pool.
type PoolStats struct {
	TotalPages   int     // total pages in pool (all states)
	IdlePages    int     // pages available for operations
	ClosingPages int     // pages in grace period, draining
	ActiveOps    int64   // total active operations across all pages
	TotalUse     int64   // cumulative use count across all pages
}

// Stats returns a snapshot of the pool's runtime statistics.
func (p *PagePool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var idle, closing int
	var totalUse int64
	for _, pp := range p.pages {
		switch pp.getState() {
		case pageIdle:
			idle++
		case pageClosing:
			closing++
		}
		totalUse += pp.useCount.Load()
	}

	return PoolStats{
		TotalPages:   len(p.pages),
		IdlePages:    idle,
		ClosingPages: closing,
		ActiveOps:    p.ActiveOps(),
		TotalUse:     totalUse,
	}
}

// IsHealthy returns true if the pool has at least one idle page.
func (p *PagePool) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, pp := range p.pages {
		if pp.getState() == pageIdle && !pp.page.IsClosed() {
			return true
		}
	}
	return len(p.pages) == 0 // empty pool is considered healthy (not yet started)
}

// HealthCheck performs a health check on all pages and returns the count of unhealthy pages.
func (p *PagePool) HealthCheck(ctx context.Context) int {
	p.mu.RLock()
	snapshot := make([]*poolPage, len(p.pages))
	copy(snapshot, p.pages)
	p.mu.RUnlock()

	var unhealthy int
	for _, pp := range snapshot {
		if pp.getState() != pageIdle {
			continue
		}
		if !p.isPageHealthy(pp) {
			unhealthy++
		}
	}
	return unhealthy
}
