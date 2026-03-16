package browserpm

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSessionState_String(t *testing.T) {
	states := []SessionState{
		SessionActive,
		SessionDegraded,
		SessionClosed,
	}

	for _, state := range states {
		s := state.String()
		if s == "" {
			t.Errorf("expected non-empty string for state %d", state)
		}
		t.Logf("State %d = %s", state, s)
	}
}

func TestPoolPageState_String(t *testing.T) {
	states := []pageState{
		pageIdle,
		pageClosing,
		pageClosed,
	}

	for _, state := range states {
		if state < 0 || state > 2 {
			t.Errorf("unexpected state value: %d", state)
		}
	}
}

func TestErrorCreation(t *testing.T) {
	t.Run("NewError", func(t *testing.T) {
		err := NewError(ErrSessionNotFound, "test session not found")
		if err.Code != ErrSessionNotFound {
			t.Errorf("expected ErrSessionNotFound, got %v", err.Code)
		}
		if err.Message != "test session not found" {
			t.Errorf("unexpected message: %s", err.Message)
		}
	})

	t.Run("WrapError", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := WrapError(cause, ErrInternal, "wrapped error")
		if err.Cause != cause {
			t.Error("expected cause to be preserved")
		}
		if err.Code != ErrInternal {
			t.Errorf("expected ErrInternal, got %v", err.Code)
		}
	})

	t.Run("Error chain", func(t *testing.T) {
		root := errors.New("root")
		l1 := WrapError(root, ErrInternal, "level 1")
		l2 := WrapError(l1, ErrContextDead, "level 2")

		if !errors.Is(l2, ErrContextDeadErr) {
			t.Error("expected l2 to match ErrContextDeadErr")
		}

		var bpmErr *BpmError
		if !errors.As(l2, &bpmErr) {
			t.Error("expected l2 to be BpmError")
		}
	})
}

func TestConfigOptions_Application(t *testing.T) {
	opts := []Option{
		WithHeadless(false),
		WithMinPages(5),
		WithMaxPages(20),
		WithPoolTTL(1 * time.Hour),
		WithGracePeriod(30 * time.Second),
		WithOperationTimeout(60 * time.Second),
		WithScheduleStrategy("round-robin"),
	}

	cfg := NewConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.Pool.MinPages != 5 {
		t.Errorf("expected MinPages 5, got %d", cfg.Pool.MinPages)
	}
	if cfg.Pool.MaxPages != 20 {
		t.Errorf("expected MaxPages 20, got %d", cfg.Pool.MaxPages)
	}
	if cfg.Pool.TTL != 1*time.Hour {
		t.Errorf("expected TTL 1h, got %v", cfg.Pool.TTL)
	}
}

func TestSessionOptions_Application(t *testing.T) {
	opts := []SessionOption{
		WithSessionMinPages(3),
		WithSessionMaxPages(10),
		WithSessionTTL(45 * time.Minute),
		WithSessionGracePeriod(15 * time.Second),
		WithSessionOperationTimeout(45 * time.Second),
	}

	var cfg PoolConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.MinPages != 3 {
		t.Errorf("expected MinPages 3, got %d", cfg.MinPages)
	}
	if cfg.MaxPages != 10 {
		t.Errorf("expected MaxPages 10, got %d", cfg.MaxPages)
	}
	if cfg.TTL != 45*time.Minute {
		t.Errorf("expected TTL 45m, got %v", cfg.TTL)
	}
}

func TestPoolConfig_Validation(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := PoolConfig{
			MinPages:         2,
			MaxPages:         5,
			TTL:              30 * time.Minute,
			GracePeriod:      10 * time.Second,
			OperationTimeout: 30 * time.Second,
			InitTimeout:      10 * time.Second,
		}

		if cfg.MinPages > cfg.MaxPages {
			t.Error("MinPages should not exceed MaxPages")
		}
	})

	t.Run("invalid min/max", func(t *testing.T) {
		cfg := PoolConfig{
			MinPages: 10,
			MaxPages: 5,
		}

		if cfg.MinPages <= cfg.MaxPages {
			t.Error("expected MinPages > MaxPages for this test case")
		}
	})
}

func TestPoolPage_AvailabilityLogic(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		state     pageState
		createdAt time.Time
		ttl       time.Duration
		grace     time.Duration
		expected  bool
	}{
		{
			name:      "idle_and_fresh",
			state:     pageIdle,
			createdAt: now.Add(-5 * time.Minute),
			ttl:       30 * time.Minute,
			grace:     5 * time.Minute,
			expected:  true,
		},
		{
			name:      "idle_but_aging",
			state:     pageIdle,
			createdAt: now.Add(-26 * time.Minute),
			ttl:       30 * time.Minute,
			grace:     5 * time.Minute,
			expected:  false,
		},
		{
			name:      "closing_state",
			state:     pageClosing,
			createdAt: now.Add(-5 * time.Minute),
			ttl:       30 * time.Minute,
			grace:     5 * time.Minute,
			expected:  false,
		},
		{
			name:      "closed_state",
			state:     pageClosed,
			createdAt: now.Add(-5 * time.Minute),
			ttl:       30 * time.Minute,
			grace:     5 * time.Minute,
			expected:  false,
		},
		{
			name:      "zero_ttl",
			state:     pageIdle,
			createdAt: now.Add(-1 * time.Hour),
			ttl:       0,
			grace:     5 * time.Minute,
			expected:  true,
		},
		{
			name:      "zero_grace",
			state:     pageIdle,
			createdAt: now.Add(-26 * time.Minute),
			ttl:       30 * time.Minute,
			grace:     0,
			expected:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pp := &poolPage{createdAt: tc.createdAt}
			pp.lastUsed.Store(now.UnixNano())
			pp.setState(tc.state)

			result := pp.isAvailable(now, tc.ttl, tc.grace)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestRoundRobinScheduler_Sequence(t *testing.T) {
	pages := make([]*poolPage, 3)
	for i := range pages {
		pages[i] = &poolPage{id: string(rune('a' + i))}
	}

	scheduler := NewScheduler("round-robin")

	sequence := make([]string, 9)
	for i := 0; i < 9; i++ {
		pp := scheduler.Select(pages)
		if pp != nil {
			sequence[i] = pp.id
		}
	}

	expected := []string{"a", "b", "c", "a", "b", "c", "a", "b", "c"}
	for i, exp := range expected {
		if sequence[i] != exp {
			t.Errorf("at position %d: expected %s, got %s", i, exp, sequence[i])
		}
	}
}

func TestScheduler_WithRemovedPages(t *testing.T) {
	pages := make([]*poolPage, 5)
	for i := range pages {
		pages[i] = &poolPage{id: string(rune('0' + i))}
	}

	scheduler := NewScheduler("round-robin")

	_ = scheduler.Select(pages)
	_ = scheduler.Select(pages)

	filtered := pages[2:]

	pp := scheduler.Select(filtered)
	if pp == nil {
		t.Error("expected non-nil selection from filtered list")
	}
}

func TestConcurrentPoolPageOperations(t *testing.T) {
	pp := &poolPage{createdAt: time.Now()}
	pp.lastUsed.Store(pp.createdAt.UnixNano())

	var wg sync.WaitGroup
	ops := 1000

	for i := 0; i < ops; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pp.activeOps.Add(1)
			pp.useCount.Add(1)
			_ = pp.getState()
			pp.setState(pageIdle)
			pp.lastUsed.Store(time.Now().UnixNano())
		}()
	}

	wg.Wait()

	if pp.activeOps.Load() != int64(ops) {
		t.Errorf("expected %d active ops, got %d", ops, pp.activeOps.Load())
	}

	if pp.useCount.Load() != int64(ops) {
		t.Errorf("expected %d use count, got %d", ops, pp.useCount.Load())
	}
}
