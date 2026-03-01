package browserpm

import (
	"context"
	"time"
)

// startHealthChecker launches a background goroutine that periodically
// checks every page in the pool using both system-level (IsClosed) and
// business-level (PageProvider.Check) checks. Unhealthy pages are
// replaced transparently.
func (p *PagePool) startHealthChecker() {
	if p.config.HealthCheckInterval <= 0 {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.config.HealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ticker.C:
				p.runHealthCheck()
			}
		}
	}()
}

func (p *PagePool) runHealthCheck() {
	p.mu.RLock()
	snapshot := make([]*poolPage, len(p.pages))
	copy(snapshot, p.pages)
	p.mu.RUnlock()

	for _, pp := range snapshot {
		if pp.getState() != pageIdle {
			continue
		}
		if !p.isPageHealthy(pp) {
			p.log.Warn("health check failed, replacing page", String("page_id", pp.id))
			p.waitAndReplace(pp)
		}
	}
}

func (p *PagePool) isPageHealthy(pp *poolPage) bool {
	if pp.page.IsClosed() {
		return false
	}
	checkCtx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	return p.provider.Check(checkCtx, pp.page)
}

// waitAndReplace waits for active ops to drain (up to GracePeriod), then
// replaces the page.
func (p *PagePool) waitAndReplace(pp *poolPage) {
	pp.setState(pageClosing)
	deadline := time.Now().Add(p.config.GracePeriod)
	for time.Now().Before(deadline) && pp.activeOps.Load() > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	p.replacePage(pp)
}

// startReaper launches a background goroutine that recycles pages whose
// TTL has expired. Pages first enter a grace period (no new ops assigned),
// then are closed and replaced if the pool is below MinPages.
func (p *PagePool) startReaper() {
	if p.config.TTL <= 0 {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ticker.C:
				p.runReap()
			}
		}
	}()
}

func (p *PagePool) runReap() {
	now := time.Now()

	p.mu.RLock()
	snapshot := make([]*poolPage, len(p.pages))
	copy(snapshot, p.pages)
	p.mu.RUnlock()

	for _, pp := range snapshot {
		age := now.Sub(pp.createdAt)
		state := pp.getState()

		switch {
		case state == pageClosed:
			continue

		case state == pageIdle && age > p.config.TTL-p.config.GracePeriod:
			pp.setState(pageClosing)
			p.log.Debug("page entering grace period", String("page_id", pp.id))

		case state == pageClosing && age > p.config.TTL:
			if pp.activeOps.Load() > 0 {
				graceDeadline := pp.createdAt.Add(p.config.TTL + p.config.GracePeriod)
				if now.Before(graceDeadline) {
					continue
				}
				p.log.Warn("force-closing page with active ops", String("page_id", pp.id), Int64("active_ops", pp.activeOps.Load()))
			}
			p.log.Info("reaping expired page", String("page_id", pp.id))
			p.mu.Lock()
			p.removePageLocked(pp)
			needMore := len(p.pages) < p.config.MinPages
			p.mu.Unlock()

			if needMore {
				if err := p.addPage(); err != nil {
					p.log.Error("failed to replenish page after reap", err)
				}
			}
		}
	}
}
