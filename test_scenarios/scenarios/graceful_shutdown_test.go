package scenarios

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
	"github.com/souloss/browserpm/test_scenarios/helpers"
)

func TestGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithGracePeriod(5*time.Second),
	)

	ts := tm.CreateSession("shutdown-test")

	for i := 0; i < 5; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
		helpers.AssertNoError(t, err)
	}

	err := ts.Session.Close()
	helpers.AssertNoError(t, err)

	helpers.AssertSessionClosed(t, ts.Session)
}

func TestGracefulShutdownWithActiveOps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithGracePeriod(3*time.Second),
	)

	ts := tm.CreateSession("active-ops-test")

	var wg sync.WaitGroup
	var started atomic.Bool
	var completed atomic.Bool

	wg.Add(1)
	go func() {
		defer wg.Done()
		started.Store(true)
		ts.DoShare(func(page playwright.Page) error {
			time.Sleep(2 * time.Second)
			_, err := page.Evaluate(`() => 1`)
			completed.Store(true)
			return err
		})
	}()

	for !started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	ts.Session.Close()
	wg.Wait()

	if !completed.Load() {
		t.Log("Operation was interrupted during shutdown")
	}
}

func TestMultipleCloseSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)
	ts := tm.CreateSession("multi-close-test")

	for i := 0; i < 3; i++ {
		err := ts.Session.Close()
		helpers.AssertNoError(t, err)
	}

	helpers.AssertSessionClosed(t, ts.Session)
}

func TestManagerCloseClosesAllSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager, err := browserpm.New(
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
	)
	helpers.AssertNoError(t, err)
	defer manager.Close()

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{},
		func(ctx context.Context, bCtx playwright.BrowserContext) error { return nil },
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error { return nil },
		func(ctx context.Context, page playwright.Page) bool { return !page.IsClosed() },
	)

	sessionNames := []string{"s1", "s2", "s3"}
	for _, name := range sessionNames {
		_, err := manager.CreateSession(name, cp, pp)
		helpers.AssertNoError(t, err)
	}

	sessions := manager.ListSessions()
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	err = manager.Close()
	helpers.AssertNoError(t, err)

	sessions = manager.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after close, got %d", len(sessions))
	}
}

func TestOperationsAfterClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)
	ts := tm.CreateSession("after-close-test")

	ts.Session.Close()

	err := ts.Do(func(page playwright.Page) error {
		_, err := page.Evaluate(`() => 1`)
		return err
	})

	helpers.AssertErrorCode(t, err, browserpm.ErrClosed)
}

func TestGracefulShutdownTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(2),
		browserpm.WithGracePeriod(1*time.Second),
	)

	ts := tm.CreateSession("shutdown-timeout-test")

	var wg sync.WaitGroup
	var opStarted atomic.Bool

	wg.Add(1)
	go func() {
		defer wg.Done()
		opStarted.Store(true)
		ts.DoShare(func(page playwright.Page) error {
			time.Sleep(5 * time.Second)
			return nil
		})
	}()

	for !opStarted.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	ts.Session.Close()
	elapsed := time.Since(start)

	t.Logf("Shutdown took: %v", elapsed)

	if elapsed > 3*time.Second {
		t.Logf("Shutdown took longer than expected, possibly due to active operations")
	}

	wg.Wait()
}

func TestConcurrentCloseRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)
	ts := tm.CreateSession("concurrent-close-test")

	var wg sync.WaitGroup
	var closeCount atomic.Int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts.Session.Close()
			closeCount.Add(1)
		}()
	}

	wg.Wait()

	helpers.AssertSessionClosed(t, ts.Session)
	t.Logf("Close attempts: %d", closeCount.Load())
}

func TestSessionStateTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)
	ts := tm.CreateSession("state-transition-test")

	helpers.AssertSessionActive(t, ts.Session)

	for i := 0; i < 3; i++ {
		err := ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
		helpers.AssertNoError(t, err)
		helpers.AssertSessionActive(t, ts.Session)
	}

	ts.Session.Close()
	helpers.AssertSessionClosed(t, ts.Session)
}

func TestResourceCleanupAfterClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
	)

	ts := tm.CreateSession("cleanup-test")

	for i := 0; i < 5; i++ {
		ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
	}

	stats := ts.Session.Stats()
	if stats != nil {
		t.Logf("Pages before close: %d", stats.TotalPages)
	}

	ts.Session.Close()

	stats = ts.Session.Stats()
	if stats != nil {
		t.Logf("Stats after close: %+v", stats)
	}
}

func TestManagerSessionClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
	)

	ts := tm.CreateSession("manager-close-test")

	for i := 0; i < 3; i++ {
		ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
	}

	err := tm.Manager.CloseSession("manager-close-test")
	helpers.AssertNoError(t, err)

	helpers.AssertSessionNotExists(t, tm.Manager, "manager-close-test")
}

func TestCloseNonExistentSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tm := helpers.NewTestManager(t)

	err := tm.Manager.CloseSession("non-existent")
	helpers.AssertErrorCode(t, err, browserpm.ErrSessionNotFound)
}
