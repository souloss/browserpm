package browserpm

import (
	"context"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

// SessionState indicates the health of a session.
type SessionState int

const (
	SessionActive   SessionState = iota // everything healthy
	SessionDegraded                     // context or some pages unhealthy
	SessionClosed                       // session is shut down
)

func (s SessionState) String() string {
	switch s {
	case SessionActive:
		return "active"
	case SessionDegraded:
		return "degraded"
	case SessionClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// SessionInfo is a read-only snapshot of a session's state.
type SessionInfo struct {
	Name      string
	State     SessionState
	PageCount int
	ActiveOps int64
	CreatedAt time.Time
}

// Session represents a named browser context with an associated page pool.
// All exported methods are safe for concurrent use.
type Session struct {
	name            string
	manager         *BrowserManager
	contextProvider ContextProvider
	pageProvider    PageProvider
	poolConfig      PoolConfig
	log             Logger

	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	bCtx    playwright.BrowserContext
	pool    *PagePool
	state   SessionState
	created time.Time
}

const maxRetries = 2

// Do executes op on an exclusive (single-use) page. The page is created,
// initialised via PageProvider.Init, handed to op, and then closed
// regardless of outcome. Failures trigger automatic retries including
// context rebuild if necessary.
func (s *Session) Do(ctx context.Context, op OperationFunc) error {
	if s.isClosed() {
		return NewError(ErrClosed, "session is closed")
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := s.ensureContext(); err != nil {
			lastErr = err
			continue
		}

		err := s.doExclusive(ctx, op)
		if err == nil {
			return nil
		}
		lastErr = err

		if s.isContextDead() {
			s.log.Warn("context dead during Do, rebuilding", String("session", s.name), Int("attempt", attempt))
			if rbErr := s.rebuildContext(); rbErr != nil {
				return WrapError(rbErr, ErrContextDead, "context rebuild failed")
			}
		}
	}
	return lastErr
}

func (s *Session) doExclusive(ctx context.Context, op OperationFunc) error {
	s.mu.RLock()
	bCtx := s.bCtx
	s.mu.RUnlock()
	if bCtx == nil {
		return NewError(ErrContextDead, "browser context is nil")
	}

	page, err := bCtx.NewPage()
	if err != nil {
		return WrapError(err, ErrPageUnavailable, "failed to create exclusive page")
	}
	defer page.Close()

	initCtx, cancel := context.WithTimeout(ctx, s.poolConfig.InitTimeout)
	defer cancel()
	if err := s.pageProvider.Init(initCtx, page); err != nil {
		return WrapError(err, ErrPageUnavailable, "exclusive page init failed")
	}

	return op(page)
}

// DoShare executes op on a shared page from the pool. The page is not
// closed after use — it stays in the pool for other callers. Multiple
// goroutines may run on the same page concurrently.
func (s *Session) DoShare(ctx context.Context, op OperationFunc) error {
	if s.isClosed() {
		return NewError(ErrClosed, "session is closed")
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := s.ensureContext(); err != nil {
			lastErr = err
			continue
		}

		pp, err := s.pool.Acquire(ctx)
		if err != nil {
			lastErr = err
			if s.isContextDead() {
				s.log.Warn("context dead during DoShare acquire, rebuilding",
					String("session", s.name), Int("attempt", attempt))
				if rbErr := s.rebuildContext(); rbErr != nil {
					return WrapError(rbErr, ErrContextDead, "context rebuild failed")
				}
			}
			continue
		}

		err = op(pp.page)
		s.pool.Release(pp)

		if err == nil {
			return nil
		}
		lastErr = err

		if pp.page.IsClosed() || s.isContextDead() {
			s.log.Warn("page/context issue during DoShare, recovering",
				String("session", s.name), String("page_id", pp.id), Int("attempt", attempt))
			if s.isContextDead() {
				if rbErr := s.rebuildContext(); rbErr != nil {
					return WrapError(rbErr, ErrContextDead, "context rebuild failed")
				}
			} else {
				go s.pool.replacePage(pp)
			}
			continue
		}

		// Business-level error — return directly, no retry.
		return err
	}
	return lastErr
}

// Status returns a snapshot of the session's current state.
func (s *Session) Status() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info := SessionInfo{
		Name:      s.name,
		State:     s.state,
		CreatedAt: s.created,
	}
	if s.pool != nil {
		info.PageCount = s.pool.Size()
		info.ActiveOps = s.pool.ActiveOps()
	}
	return info
}

// IsHealthy returns true if the session is active and has at least one healthy page.
func (s *Session) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state == SessionClosed {
		return false
	}
	if s.pool == nil {
		// Pool not yet initialized, consider healthy
		return s.state == SessionActive
	}
	return s.pool.IsHealthy()
}

// HealthCheck performs a health check on all pages in the pool.
// Returns the number of unhealthy pages found.
func (s *Session) HealthCheck(ctx context.Context) int {
	s.mu.RLock()
	pool := s.pool
	s.mu.RUnlock()

	if pool == nil {
		return 0
	}
	return pool.HealthCheck(ctx)
}

// Stats returns runtime statistics for the session's page pool.
func (s *Session) Stats() *PoolStats {
	s.mu.RLock()
	pool := s.pool
	s.mu.RUnlock()

	if pool == nil {
		return nil
	}
	stats := pool.Stats()
	return &stats
}

// Close shuts down the session, drains active operations, and releases
// all resources (pages, context).
func (s *Session) Close() error {
	s.mu.Lock()
	if s.state == SessionClosed {
		s.mu.Unlock()
		return nil
	}
	s.state = SessionClosed
	pool := s.pool
	bCtx := s.bCtx
	s.pool = nil
	s.bCtx = nil
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	s.log.Info("closing session", String("session", s.name))

	if pool != nil {
		pool.Drain(s.poolConfig.GracePeriod)
		pool.Close()
	}
	if bCtx != nil {
		bCtx.Close()
	}
	return nil
}

// --- Internal helpers ---

func (s *Session) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == SessionClosed
}

func (s *Session) isContextDead() bool {
	s.mu.RLock()
	bCtx := s.bCtx
	s.mu.RUnlock()

	if bCtx == nil {
		return true
	}
	// Try a cheap operation; if the context is dead, Pages() will fail or
	// return nil on a closed context.
	defer func() { recover() }()
	_ = bCtx.Pages()
	return false
}

// ensureContext creates the BrowserContext and page pool if they don't exist.
func (s *Session) ensureContext() error {
	s.mu.RLock()
	if s.bCtx != nil && s.pool != nil {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.bCtx != nil && s.pool != nil {
		return nil
	}
	return s.buildContextLocked()
}

// rebuildContext tears down and rebuilds the context and pool.
func (s *Session) rebuildContext() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("rebuilding context", String("session", s.name))

	if s.pool != nil {
		s.pool.Close()
		s.pool = nil
	}
	if s.bCtx != nil {
		s.bCtx.Close()
		s.bCtx = nil
	}

	return s.buildContextLocked()
}

// buildContextLocked creates a new BrowserContext and PagePool.
// Caller must hold s.mu write lock.
func (s *Session) buildContextLocked() error {
	opts := s.contextProvider.Options()
	bCtx, err := s.manager.browser.NewContext(opts)
	if err != nil {
		s.state = SessionDegraded
		return WrapError(err, ErrContextDead, "failed to create browser context")
	}

	setupCtx, cancel := context.WithTimeout(s.ctx, s.poolConfig.InitTimeout)
	defer cancel()
	if err := s.contextProvider.Setup(setupCtx, bCtx); err != nil {
		bCtx.Close()
		s.state = SessionDegraded
		return WrapError(err, ErrContextDead, "context setup failed")
	}

	pool := newPagePool(s.poolConfig, s.pageProvider, s.log, func() (playwright.Page, error) {
		return bCtx.NewPage()
	})
	if err := pool.Start(); err != nil {
		bCtx.Close()
		s.state = SessionDegraded
		return err
	}

	s.bCtx = bCtx
	s.pool = pool
	s.state = SessionActive
	s.log.Info("context built", String("session", s.name), Int("pages", pool.Size()))
	return nil
}
