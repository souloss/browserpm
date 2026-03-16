package browserpm

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolPageState(t *testing.T) {
	pp := &poolPage{
		createdAt: time.Now(),
	}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	if pp.getState() != pageIdle {
		t.Errorf("expected initial state to be pageIdle, got %d", pp.getState())
	}

	pp.setState(pageClosing)
	if pp.getState() != pageClosing {
		t.Errorf("expected state to be pageClosing, got %d", pp.getState())
	}

	pp.setState(pageClosed)
	if pp.getState() != pageClosed {
		t.Errorf("expected state to be pageClosed, got %d", pp.getState())
	}
}

func TestPoolPageIsAvailable(t *testing.T) {
	now := time.Now()

	t.Run("idle state is available", func(t *testing.T) {
		pp := &poolPage{createdAt: now}
		pp.lastUsed.Store(now.UnixNano())

		if !pp.isAvailable(now, 0, 0) {
			t.Error("expected idle page to be available")
		}
	})

	t.Run("closing state is not available", func(t *testing.T) {
		pp := &poolPage{createdAt: now}
		pp.lastUsed.Store(now.UnixNano())
		pp.setState(pageClosing)

		if pp.isAvailable(now, 0, 0) {
			t.Error("expected closing page to not be available")
		}
	})

	t.Run("closed state is not available", func(t *testing.T) {
		pp := &poolPage{createdAt: now}
		pp.lastUsed.Store(now.UnixNano())
		pp.setState(pageClosed)

		if pp.isAvailable(now, 0, 0) {
			t.Error("expected closed page to not be available")
		}
	})

	t.Run("page near TTL grace period is not available", func(t *testing.T) {
		ttl := 30 * time.Minute
		grace := 5 * time.Minute
		pp := &poolPage{createdAt: now.Add(-26 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		if pp.isAvailable(now, ttl, grace) {
			t.Error("expected page near TTL to not be available")
		}
	})

	t.Run("page within TTL is available", func(t *testing.T) {
		ttl := 30 * time.Minute
		grace := 5 * time.Minute
		pp := &poolPage{createdAt: now.Add(-10 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		if !pp.isAvailable(now, ttl, grace) {
			t.Error("expected page within TTL to be available")
		}
	})
}

func TestPoolPageActiveOps(t *testing.T) {
	pp := &poolPage{}

	if pp.activeOps.Load() != 0 {
		t.Errorf("expected initial activeOps to be 0, got %d", pp.activeOps.Load())
	}

	pp.activeOps.Add(1)
	if pp.activeOps.Load() != 1 {
		t.Errorf("expected activeOps to be 1, got %d", pp.activeOps.Load())
	}

	pp.activeOps.Add(2)
	if pp.activeOps.Load() != 3 {
		t.Errorf("expected activeOps to be 3, got %d", pp.activeOps.Load())
	}

	pp.activeOps.Add(-3)
	if pp.activeOps.Load() != 0 {
		t.Errorf("expected activeOps to be 0 after decrement, got %d", pp.activeOps.Load())
	}
}

func TestPoolPageUseCount(t *testing.T) {
	pp := &poolPage{}

	for i := 0; i < 10; i++ {
		pp.useCount.Add(1)
	}

	if pp.useCount.Load() != 10 {
		t.Errorf("expected useCount to be 10, got %d", pp.useCount.Load())
	}
}

func TestPoolPageConcurrentOps(t *testing.T) {
	pp := &poolPage{}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pp.activeOps.Add(1)
			pp.useCount.Add(1)
		}()
	}
	wg.Wait()

	if pp.activeOps.Load() != 100 {
		t.Errorf("expected activeOps to be 100, got %d", pp.activeOps.Load())
	}

	if pp.useCount.Load() != 100 {
		t.Errorf("expected useCount to be 100, got %d", pp.useCount.Load())
	}
}

func TestPoolStatsEmpty(t *testing.T) {
	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		ScheduleStrategy: "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	stats := pool.Stats()

	if stats.TotalPages != 0 {
		t.Errorf("expected total pages to be 0, got %d", stats.TotalPages)
	}

	if stats.IdlePages != 0 {
		t.Errorf("expected idle pages to be 0, got %d", stats.IdlePages)
	}

	if stats.ActiveOps != 0 {
		t.Errorf("expected activeOps to be 0, got %d", stats.ActiveOps)
	}
}

func TestPoolActiveOpsConcurrent(t *testing.T) {
	pp1 := &poolPage{}
	pp2 := &poolPage{}
	pp3 := &poolPage{}

	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		ScheduleStrategy: "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)
	pool.pages = []*poolPage{pp1, pp2, pp3}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			pp1.activeOps.Add(1)
		}()
		go func() {
			defer wg.Done()
			pp2.activeOps.Add(1)
		}()
		go func() {
			defer wg.Done()
			pp3.activeOps.Add(1)
		}()
	}
	wg.Wait()

	total := pool.ActiveOps()
	if total != 300 {
		t.Errorf("expected total activeOps to be 300, got %d", total)
	}
}

func TestPoolSchedulerRoundRobin(t *testing.T) {
	pages := make([]*poolPage, 5)
	for i := range pages {
		pages[i] = &poolPage{id: string(rune('a' + i))}
	}

	scheduler := NewScheduler("round-robin")

	selected := make(map[string]int)
	for i := 0; i < 25; i++ {
		pp := scheduler.Select(pages)
		if pp != nil {
			selected[pp.id]++
		}
	}

	for id, count := range selected {
		if count != 5 {
			t.Errorf("expected each page to be selected 5 times, got %d for %s", count, id)
		}
	}
}

func TestPoolSchedulerEmpty(t *testing.T) {
	scheduler := NewScheduler("round-robin")

	pp := scheduler.Select(nil)
	if pp != nil {
		t.Error("expected nil for empty page list")
	}

	pp = scheduler.Select([]*poolPage{})
	if pp != nil {
		t.Error("expected nil for empty page slice")
	}
}

func TestPoolSchedulerReset(t *testing.T) {
	pages := make([]*poolPage, 3)
	for i := range pages {
		pages[i] = &poolPage{id: string(rune('a' + i))}
	}

	scheduler := NewScheduler("round-robin")

	for i := 0; i < 5; i++ {
		_ = scheduler.Select(pages)
	}

	scheduler.Reset()

	pp := scheduler.Select(pages)
	if pp == nil || pp.id != "a" {
		t.Error("expected reset to start from first page again")
	}
}

func TestPoolIsHealthyEmpty(t *testing.T) {
	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		ScheduleStrategy: "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	if !pool.IsHealthy() {
		t.Error("expected empty pool to be healthy")
	}
}

func TestPoolPageTTLTransition(t *testing.T) {
	now := time.Now()
	ttl := 30 * time.Minute
	grace := 5 * time.Minute

	pp := &poolPage{createdAt: now.Add(-15 * time.Minute)}
	pp.lastUsed.Store(now.UnixNano())

	if !pp.isAvailable(now, ttl, grace) {
		t.Error("expected page within TTL to be available")
	}

	pp = &poolPage{createdAt: now.Add(-26 * time.Minute)}
	pp.lastUsed.Store(now.UnixNano())

	if pp.isAvailable(now, ttl, grace) {
		t.Error("expected page near TTL grace period to not be available")
	}

	pp = &poolPage{createdAt: now.Add(-35 * time.Minute)}
	pp.lastUsed.Store(now.UnixNano())

	if pp.isAvailable(now, ttl, grace) {
		t.Error("expected page past TTL to not be available")
	}
}

func TestPoolPageStateTransitions(t *testing.T) {
	pp := &poolPage{createdAt: time.Now()}
	pp.lastUsed.Store(time.Now().UnixNano())

	if pp.getState() != pageIdle {
		t.Errorf("expected initial state to be pageIdle, got %d", pp.getState())
	}

	pp.setState(pageClosing)
	if pp.getState() != pageClosing {
		t.Errorf("expected state to be pageClosing, got %d", pp.getState())
	}

	pp.setState(pageClosed)
	if pp.getState() != pageClosed {
		t.Errorf("expected state to be pageClosed, got %d", pp.getState())
	}

	pp.setState(pageIdle)
	if pp.getState() != pageIdle {
		t.Errorf("expected state to return to pageIdle, got %d", pp.getState())
	}
}

func TestPoolPageLastUsed(t *testing.T) {
	pp := &poolPage{createdAt: time.Now()}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	initialTime := pp.lastUsed.Load()
	time.Sleep(10 * time.Millisecond)

	newTime := time.Now().UnixNano()
	pp.lastUsed.Store(newTime)

	if pp.lastUsed.Load() == initialTime {
		t.Error("expected lastUsed to be updated")
	}

	if pp.lastUsed.Load() != newTime {
		t.Error("expected lastUsed to match new time")
	}
}

func TestPoolPageConcurrentStateChange(t *testing.T) {
	pp := &poolPage{createdAt: time.Now()}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	var wg sync.WaitGroup
	var stateChanges atomic.Int64

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state := pageState(i % 3)
			pp.setState(state)
			stateChanges.Add(1)
		}(i)
	}
	wg.Wait()

	if stateChanges.Load() != 100 {
		t.Errorf("expected 100 state changes, got %d", stateChanges.Load())
	}
}

func TestPoolConfigDefaults(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if cfg.Pool.MinPages <= 0 {
		t.Error("expected default MinPages to be positive")
	}

	if cfg.Pool.MaxPages < cfg.Pool.MinPages {
		t.Error("expected MaxPages >= MinPages")
	}

	if cfg.Pool.TTL <= 0 {
		t.Error("expected TTL to be positive")
	}

	if cfg.Pool.GracePeriod <= 0 {
		t.Error("expected GracePeriod to be positive")
	}
}

func TestPoolErrorWrapping(t *testing.T) {
	originalErr := errors.New("original error")
	wrapped := WrapError(originalErr, ErrInternal, "pool error")

	if wrapped.Code != ErrInternal {
		t.Errorf("expected error code ErrInternal, got %v", wrapped.Code)
	}

	if wrapped.Cause != originalErr {
		t.Error("expected cause to be original error")
	}

	var bpmErr *BpmError
	if !errors.As(wrapped, &bpmErr) {
		t.Error("expected error to be BpmError")
	}
}

func TestPoolPageZeroTTLGrace(t *testing.T) {
	now := time.Now()
	pp := &poolPage{createdAt: now.Add(-1 * time.Hour)}
	pp.lastUsed.Store(now.UnixNano())

	if !pp.isAvailable(now, 0, 0) {
		t.Error("expected page to be available with zero TTL/grace")
	}

	if !pp.isAvailable(now, 0, 10*time.Second) {
		t.Error("expected page to be available with zero TTL")
	}

	if !pp.isAvailable(now, 10*time.Second, 0) {
		t.Error("expected page to be available with zero grace")
	}
}

func TestPoolSchedulerConcurrent(t *testing.T) {
	pages := make([]*poolPage, 10)
	for i := range pages {
		pages[i] = &poolPage{id: string(rune('0' + i))}
	}

	scheduler := NewScheduler("round-robin")

	var wg sync.WaitGroup
	var mu sync.Mutex
	selected := make(map[string]int)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pp := scheduler.Select(pages)
			if pp != nil {
				mu.Lock()
				selected[pp.id]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	totalSelected := 0
	for _, count := range selected {
		totalSelected += count
	}

	if totalSelected != 100 {
		t.Errorf("expected 100 selections, got %d", totalSelected)
	}
}
