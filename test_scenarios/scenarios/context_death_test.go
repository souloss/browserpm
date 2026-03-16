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

func TestContextDeathDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("context-death-test")

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		return err
	})
	helpers.AssertNoError(t, err)

	info := ts.Session.Status()
	helpers.AssertSessionActive(t, ts.Session)
	t.Logf("Session state: %s, pages: %d", info.State, info.PageCount)
}

func TestContextDeathRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
		browserpm.WithInitTimeout(30*time.Second),
	)

	ts := tm.CreateSession("context-recovery-test")

	for i := 0; i < 5; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	helpers.AssertSessionActive(t, ts.Session)
	helpers.AssertSessionHealthy(t, ts.Session)
}

func TestContextDeathDuringOperation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("context-death-op-test")

	var successCount atomic.Int64
	var recoveryCount atomic.Int64

	for i := 0; i < 10; i++ {
		err := ts.Do(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Math.random()`)
			return err
		})

		if err == nil {
			successCount.Add(1)
		} else {
			var bpmErr *browserpm.BpmError
			if errors.As(err, &bpmErr) {
				if bpmErr.Code == browserpm.ErrContextDead {
					recoveryCount.Add(1)
				}
			}
		}
	}

	t.Logf("Successes: %d, Recoveries: %d", successCount.Load(), recoveryCount.Load())
}

func TestConcurrentContextDeath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("concurrent-context-test")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var errorCount atomic.Int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				opCount.Add(1)
				err := ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => performance.now()`)
					return err
				})

				if err != nil {
					errorCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Operations: %d, Errors: %d", opCount.Load(), errorCount.Load())
	helpers.AssertSessionActive(t, ts.Session)
}

func TestContextRebuildProtection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("rebuild-protection-test")

	for i := 0; i < 3; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	info := ts.Session.Status()
	helpers.AssertSessionActive(t, ts.Session)
	t.Logf("Session: %s, Pages: %d", info.State, info.PageCount)
}

func TestContextDeathWithPoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(2),
		browserpm.WithOperationTimeout(10*time.Second),
	)

	ts := tm.CreateSession("context-exhaustion-test")

	var wg sync.WaitGroup
	var exhaustedCount atomic.Int64
	var successCount atomic.Int64

	blockChan := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
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
		}()
	}

	close(blockChan)
	wg.Wait()

	t.Logf("Successes: %d, Pool exhausted: %d", successCount.Load(), exhaustedCount.Load())
}

func TestContextStateTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithGracePeriod(2*time.Second),
	)

	ts := tm.CreateSession("state-transition-test")

	helpers.AssertSessionActive(t, ts.Session)

	for i := 0; i < 3; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})
		helpers.AssertNoError(t, err)
		helpers.AssertSessionActive(t, ts.Session)
	}

	ts.Session.Close()
	helpers.AssertSessionClosed(t, ts.Session)
}

func TestContextClosedBeforeOperation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)
	ts := tm.CreateSession("closed-before-op-test")

	ts.Session.Close()

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Evaluate(`() => 1`)
		return err
	})

	helpers.AssertErrorCode(t, err, browserpm.ErrClosed)
}

func TestContextMultipleRebuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("multi-rebuild-test")

	for round := 0; round < 3; round++ {
		for i := 0; i < 5; i++ {
			err := ts.DoShare(func(page playwright.Page) error {
				_, err := page.Evaluate(`() => ({ round: performance.now() })`)
				return err
			})

			if err != nil {
				t.Logf("Round %d, op %d error: %v", round, i, err)
			}
		}

		t.Logf("Round %d complete, session state: %s", round, ts.Session.Status().State)
	}

	helpers.AssertSessionActive(t, ts.Session)
	helpers.AssertSessionHealthy(t, ts.Session)
}

func TestContextHealthAfterRebuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithHealthCheckInterval(5*time.Second),
	)

	ts := tm.CreateSession("health-after-rebuild-test")

	for i := 0; i < 5; i++ {
		err := ts.Do(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	unhealthy := ts.Session.HealthCheck(context.Background())
	t.Logf("Unhealthy pages: %d", unhealthy)

	helpers.AssertSessionActive(t, ts.Session)
	helpers.AssertSessionHealthy(t, ts.Session)
}
