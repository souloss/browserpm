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

func TestBrowserCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("crash-test")

	var opCount atomic.Int64
	var successCount atomic.Int64
	var recoverCount atomic.Int64

	for i := 0; i < 5; i++ {
		err := ts.Do(func(page playwright.Page) error {
			opCount.Add(1)
			_, err := page.Evaluate(`() => 1 + 1`)
			if err == nil {
				successCount.Add(1)
			}
			return err
		})

		if err != nil {
			var bpmErr *browserpm.BpmError
			if errors.As(err, &bpmErr) {
				if bpmErr.Code == browserpm.ErrContextDead {
					recoverCount.Add(1)
				}
			}
		}
	}

	helpers.AssertSessionActive(t, ts.Session)
	t.Logf("Operations: %d, Successes: %d, Recoveries: %d",
		opCount.Load(), successCount.Load(), recoverCount.Load())
}

func TestBrowserCrashDuringConcurrentOps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("concurrent-crash-test")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var errorCount atomic.Int64

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				opCount.Add(1)
				err := ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => Math.random()`)
					return err
				})

				if err != nil {
					errorCount.Add(1)
				}

				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Total operations: %d, Errors: %d", opCount.Load(), errorCount.Load())
	helpers.AssertSessionActive(t, ts.Session)
}

func TestContextRebuildAfterCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("rebuild-test")

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		return err
	})
	helpers.AssertNoError(t, err)

	info := ts.Session.Status()
	t.Logf("Session state after first op: %s, pages: %d", info.State, info.PageCount)

	for i := 0; i < 10; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => document.title`)
			return err
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	helpers.AssertSessionActive(t, ts.Session)
}

func TestCrashWithGracefulDegradation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithGracePeriod(5*time.Second),
	)

	ts := tm.CreateSession("graceful-test")

	for i := 0; i < 5; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			time.Sleep(100 * time.Millisecond)
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})

		if err != nil {
			t.Logf("Operation %d error: %v", i, err)
		}
	}

	ts.Session.Close()

	helpers.AssertSessionClosed(t, ts.Session)
}

func TestCrashDuringPageInitialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	crashInInit := atomic.Bool{}

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-crash-test/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			return nil
		},
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			if crashInInit.Load() {
				return errors.New("target closed: simulated crash during init")
			}
			return nil
		},
		func(ctx context.Context, page playwright.Page) bool {
			return !page.IsClosed()
		},
	)

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithInitTimeout(10*time.Second),
	)

	session, err := tm.Manager.CreateSession("init-crash-test", cp, pp)
	helpers.AssertNoError(t, err)
	defer session.Close()

	err = session.Do(context.Background(), func(page playwright.Page) error {
		_, err := page.Evaluate(`() => 1`)
		return err
	})
	helpers.AssertNoError(t, err)

	crashInInit.Store(true)

	err = session.Do(context.Background(), func(page playwright.Page) error {
		_, err := page.Evaluate(`() => 2`)
		return err
	})

	t.Logf("Error after crash simulation: %v", err)

	crashInInit.Store(false)
}
