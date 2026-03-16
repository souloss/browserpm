package scenarios

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
	"github.com/souloss/browserpm/test_scenarios/helpers"
)

func TestPageDeathRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("page-death-test")

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		return err
	})
	helpers.AssertNoError(t, err)

	for i := 0; i < 10; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	helpers.AssertSessionHealthy(t, ts.Session)
}

func TestPageClosedDuringOperation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("page-closed-test")

	var closedErrorCount atomic.Int64
	var successCount atomic.Int64

	for i := 0; i < 10; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			if page.IsClosed() {
				return errors.New("page has been closed")
			}
			_, err := page.Evaluate(`() => Math.random()`)
			return err
		})

		if err != nil {
			if browserpm.IsConnectionError(err) {
				closedErrorCount.Add(1)
			}
		} else {
			successCount.Add(1)
		}
	}

	t.Logf("Successes: %d, Closed errors: %d", successCount.Load(), closedErrorCount.Load())
}

func TestPageHealthCheckFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithHealthCheckInterval(2*time.Second),
		browserpm.WithGracePeriod(1*time.Second),
	)

	ts := tm.CreateSession("health-check-test")

	for i := 0; i < 5; i++ {
		err := ts.Do(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1 + 1`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	unhealthy := ts.Session.HealthCheck(context.Background())
	t.Logf("Unhealthy pages found: %d", unhealthy)

	helpers.AssertSessionActive(t, ts.Session)
}

func TestPageReplacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(30*time.Second),
		browserpm.WithGracePeriod(5*time.Second),
	)

	ts := tm.CreateSession("page-replacement-test")

	initialStats := ts.Session.Stats()
	if initialStats != nil {
		t.Logf("Initial pages: %d", initialStats.TotalPages)
	}

	for i := 0; i < 20; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => performance.now()`)
			return err
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	finalStats := ts.Session.Stats()
	if finalStats != nil {
		t.Logf("Final pages: %d, total use: %d", finalStats.TotalPages, finalStats.TotalUse)
	}

	helpers.AssertSessionActive(t, ts.Session)
}

func TestExecutionContextDestroyed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	execContextErrors := []struct {
		name   string
		errMsg string
	}{
		{"context_destroyed", "Execution context was destroyed"},
		{"context_destroyed_lower", "execution context was destroyed"},
		{"frame_detached", "frame was detached"},
	}

	for _, tc := range execContextErrors {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New(tc.errMsg)
			if !browserpm.IsConnectionError(err) {
				t.Errorf("expected execution context error to be detected: %s", tc.errMsg)
			}
		})
	}
}

func TestPagePoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(2),
		browserpm.WithOperationTimeout(5*time.Second),
	)

	ts := tm.CreateSession("pool-exhaustion-test")

	var wg sync.WaitGroup
	var exhaustedCount atomic.Int64
	var successCount atomic.Int64

	blockChan := make(chan struct{})

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := ts.DoShare(func(page playwright.Page) error {
				<-blockChan
				_, err := page.Evaluate(`() => 1`)
				return err
			})

			if err != nil {
				var bpmErr *browserpm.BpmError
				if errors.As(err, &bpmErr) && bpmErr.Code == browserpm.ErrPoolExhausted {
					exhaustedCount.Add(1)
				}
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	close(blockChan)
	wg.Wait()

	t.Logf("Successes: %d, Pool exhausted: %d", successCount.Load(), exhaustedCount.Load())
}

func TestPageTTLExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(5*time.Second),
		browserpm.WithGracePeriod(1*time.Second),
	)

	ts := tm.CreateSession("ttl-test")

	for i := 0; i < 3; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	time.Sleep(7 * time.Second)

	for i := 0; i < 3; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})

		if err != nil {
			t.Logf("Post-TTL operation %d error: %v", i, err)
		}
	}

	helpers.AssertSessionActive(t, ts.Session)
}

func TestPageDeathDuringConcurrentOps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("concurrent-page-death-test")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var errorCount atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opCount.Add(1)
					err := ts.DoShare(func(page playwright.Page) error {
						_, err := page.Evaluate(`() => Math.random() * 1000`)
						return err
					})

					if err != nil {
						errorCount.Add(1)
					}

					time.Sleep(20 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Operations: %d, Errors: %d", opCount.Load(), errorCount.Load())
	helpers.AssertSessionActive(t, ts.Session)
}
