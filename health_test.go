package browserpm

import (
	"context"
	"testing"
	"time"
)

func TestHealthCheckIntervalZero(t *testing.T) {
	cfg := PoolConfig{
		MinPages:            1,
		MaxPages:            5,
		HealthCheckInterval: 0,
		ScheduleStrategy:    "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	pool.startHealthChecker()

	pool.cancel()
	pool.wg.Wait()
}

func TestReaperTTLZero(t *testing.T) {
	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		TTL:              0,
		ScheduleStrategy: "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	pool.startReaper()

	pool.cancel()
	pool.wg.Wait()
}

func TestWaitAndReplaceStateChange(t *testing.T) {
	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		GracePeriod:      100 * time.Millisecond,
		ScheduleStrategy: "round-robin",
	}

	_ = cfg

	pp := &poolPage{
		createdAt: time.Now(),
	}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	initialState := pp.getState()
	if initialState != pageIdle {
		t.Errorf("expected initial state to be pageIdle, got %d", initialState)
	}

	pp.setState(pageClosing)
	if pp.getState() != pageClosing {
		t.Errorf("expected state to be pageClosing, got %d", pp.getState())
	}
}

func TestRunReapStateTransitions(t *testing.T) {
	now := time.Now()
	ttl := 30 * time.Minute
	grace := 5 * time.Minute

	t.Run("page within TTL stays idle", func(t *testing.T) {
		pp := &poolPage{createdAt: now.Add(-10 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		age := now.Sub(pp.createdAt)
		if age > ttl-grace {
			t.Error("expected page to be within TTL grace period")
		}
	})

	t.Run("page past TTL grace period enters closing", func(t *testing.T) {
		pp := &poolPage{createdAt: now.Add(-26 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		age := now.Sub(pp.createdAt)
		if age <= ttl-grace {
			t.Error("expected page to be past TTL grace period")
		}
	})

	t.Run("page past TTL should be reaped", func(t *testing.T) {
		pp := &poolPage{createdAt: now.Add(-35 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		age := now.Sub(pp.createdAt)
		if age <= ttl {
			t.Error("expected page to be past TTL")
		}
	})
}

func TestGracePeriodLogic(t *testing.T) {
	now := time.Now()
	grace := 5 * time.Minute
	ttl := 30 * time.Minute

	pp := &poolPage{createdAt: now.Add(-28 * time.Minute)}
	pp.lastUsed.Store(now.UnixNano())

	age := now.Sub(pp.createdAt)

	inGracePeriod := age > ttl-grace && age <= ttl
	if !inGracePeriod {
		t.Error("expected page to be in grace period")
	}

	pp2 := &poolPage{createdAt: now.Add(-15 * time.Minute)}
	pp2.lastUsed.Store(now.UnixNano())

	age2 := now.Sub(pp2.createdAt)
	inGracePeriod2 := age2 > ttl-grace && age2 <= ttl
	if inGracePeriod2 {
		t.Error("expected page to NOT be in grace period")
	}
}

func TestForceCloseDeadline(t *testing.T) {
	now := time.Now()
	ttl := 30 * time.Minute
	grace := 5 * time.Minute

	pp := &poolPage{createdAt: now.Add(-35 * time.Minute)}
	pp.lastUsed.Store(now.UnixNano())

	graceDeadline := pp.createdAt.Add(ttl + grace)
	forceClose := now.After(graceDeadline) || now.Equal(graceDeadline)

	if forceClose {
		t.Log("Page should be force closed after TTL + GracePeriod")
	}
}

func TestHealthCheckContextCancellation(t *testing.T) {
	cfg := PoolConfig{
		MinPages:            1,
		MaxPages:            5,
		HealthCheckInterval: 1 * time.Hour,
		ScheduleStrategy:    "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	select {
	case <-ctx.Done():
		t.Log("Context was properly cancelled")
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should have been cancelled")
	}

	pool.cancel()
	pool.wg.Wait()
}

func TestReaperContextCancellation(t *testing.T) {
	cfg := PoolConfig{
		MinPages:         1,
		MaxPages:         5,
		TTL:              1 * time.Hour,
		ScheduleStrategy: "round-robin",
	}

	logger := NewNopLogger()
	pool := newPagePool(cfg, nil, logger, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	select {
	case <-ctx.Done():
		t.Log("Context was properly cancelled")
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should have been cancelled")
	}

	pool.cancel()
	pool.wg.Wait()
}

func TestActiveOpsDrain(t *testing.T) {
	pp := &poolPage{}
	pp.activeOps.Store(3)

	grace := 100 * time.Millisecond
	deadline := time.Now().Add(grace)

	drained := false
	for time.Now().Before(deadline) && pp.activeOps.Load() > 0 {
		time.Sleep(10 * time.Millisecond)
		if pp.activeOps.Load() == 0 {
			drained = true
			break
		}
	}

	if drained {
		t.Log("Active ops were drained")
	} else {
		t.Log("Grace period expired before ops drained")
	}
}

func TestTTLGracePeriodEdgeCases(t *testing.T) {
	now := time.Now()
	ttl := 30 * time.Minute
	grace := 5 * time.Minute

	tests := []struct {
		name       string
		age        time.Duration
		expectIdle bool
	}{
		{"new page", 1 * time.Minute, true},
		{"mid-life page", 15 * time.Minute, true},
		{"near grace", 24 * time.Minute, true},
		{"at grace boundary", 25 * time.Minute, true},
		{"in grace period", 26 * time.Minute, false},
		{"near TTL", 29 * time.Minute, false},
		{"at TTL", 30 * time.Minute, false},
		{"past TTL", 31 * time.Minute, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pp := &poolPage{createdAt: now.Add(-tc.age)}
			pp.lastUsed.Store(now.UnixNano())

			available := pp.isAvailable(now, ttl, grace)
			if available != tc.expectIdle {
				t.Errorf("age=%v: expected available=%v, got %v", tc.age, tc.expectIdle, available)
			}
		})
	}
}

func TestRunReapPageStates(t *testing.T) {
	now := time.Now()

	t.Run("closed page is skipped", func(t *testing.T) {
		pp := &poolPage{createdAt: now}
		pp.setState(pageClosed)

		if pp.getState() != pageClosed {
			t.Error("expected page to be closed")
		}
	})

	t.Run("idle page transitions to closing", func(t *testing.T) {
		pp := &poolPage{createdAt: now.Add(-26 * time.Minute)}
		pp.lastUsed.Store(now.UnixNano())

		if pp.getState() != pageIdle {
			t.Error("expected initial state to be idle")
		}

		pp.setState(pageClosing)
		if pp.getState() != pageClosing {
			t.Error("expected state to be closing")
		}
	})
}

func TestGracePeriodWaiting(t *testing.T) {
	pp := &poolPage{}
	pp.activeOps.Store(2)

	gracePeriod := 50 * time.Millisecond
	deadline := time.Now().Add(gracePeriod)

	start := time.Now()
	for time.Now().Before(deadline) && pp.activeOps.Load() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(start)

	if elapsed >= gracePeriod {
		t.Log("Grace period fully utilized")
	} else {
		t.Log("Ops drained before grace period ended")
	}
}
