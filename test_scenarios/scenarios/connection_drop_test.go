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

func TestConnectionDropRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
		browserpm.WithInitTimeout(30*time.Second),
	)

	ts := tm.CreateSession("connection-drop-test")

	for i := 0; i < 5; i++ {
		err := ts.Do(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	helpers.AssertSessionActive(t, ts.Session)
}

func TestConnectionDropDuringOperation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("connection-drop-op-test")

	var successCount atomic.Int64
	var retryCount atomic.Int64

	for i := 0; i < 10; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Math.random()`)
			return err
		})

		if err == nil {
			successCount.Add(1)
		} else {
			var bpmErr *browserpm.BpmError
			if errors.As(err, &bpmErr) {
				if bpmErr.Code == browserpm.ErrContextDead {
					retryCount.Add(1)
				}
			}
		}
	}

	t.Logf("Successes: %d, Retries: %d", successCount.Load(), retryCount.Load())
}

func TestConnectionDropWithConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("concurrent-connection-test")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var errorCount atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opCount.Add(1)
					err := ts.DoShare(func(page playwright.Page) error {
						_, err := page.Evaluate(`() => performance.now()`)
						return err
					})

					if err != nil {
						errorCount.Add(1)
					}

					time.Sleep(50 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Operations: %d, Errors: %d", opCount.Load(), errorCount.Load())
}

func TestWebSocketClosedRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("websocket-test")

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		return err
	})
	helpers.AssertNoError(t, err)

	for i := 0; i < 5; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			result, err := page.Evaluate(`() => document.readyState`)
			if err != nil {
				return err
			}
			t.Logf("Ready state: %v", result)
			return nil
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}
}

func TestConnectionTimeoutRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(5*time.Second),
		browserpm.WithInitTimeout(10*time.Second),
	)

	ts := tm.CreateSession("timeout-test")

	slowOp := func(page playwright.Page) error {
		_, err := page.Evaluate(`() => new Promise(r => setTimeout(r, 100))`)
		return err
	}

	for i := 0; i < 5; i++ {
		err := ts.DoShare(slowOp)
		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	helpers.AssertSessionActive(t, ts.Session)
}

func TestConnectionStateAfterMultipleDrops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("multi-drop-test")

	for round := 0; round < 3; round++ {
		for i := 0; i < 3; i++ {
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
