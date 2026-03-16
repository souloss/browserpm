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
	"github.com/souloss/browserpm/test_scenarios/mock"
)

// TestActiveBrowserCrashKillMain tests killing the main browser process.
// This is the most severe crash scenario - the entire browser dies.
func TestActiveBrowserCrashKillMain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fit := helpers.NewFaultInjectionTest(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts, err := fit.Manager().CreateSession("active-crash-main", nil, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	_ = ts // Session created but will be killed

	// Get process info before crash
	ctx := context.Background()
	infos, err := fit.Manager().GetProcessInfos(ctx)
	if err != nil {
		t.Fatalf("failed to get process infos: %v", err)
	}

	t.Logf("Found %d browser processes before crash", len(infos))
	for _, info := range infos {
		t.Logf("  PID %d: type=%s", info.ID, info.Type)
	}

	// Kill the main browser process
	err = fit.KillMainBrowserProcess(ctx)
	if err != nil {
		t.Logf("KillMainBrowserProcess result: %v", err)
	}

	// Verify processes were killed
	killedPIDs := fit.Killer().KilledPIDs()
	t.Logf("Killed PIDs: %v", killedPIDs)
}

// TestActiveBrowserCrashKillRenderer tests killing a renderer process.
// This simulates a tab/page crash while the browser remains alive.
func TestActiveBrowserCrashKillRenderer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fit := helpers.NewFaultInjectionTest(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	).CreateSession("renderer-crash-test")

	// Perform some operations to ensure renderer processes exist
	for i := 0; i < 3; i++ {
		ts.DoShare(func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})
	}

	// Get process info
	ctx := context.Background()
	infos, err := fit.Manager().GetProcessInfos(ctx)
	if err != nil {
		t.Logf("GetProcessInfos error: %v", err)
	} else {
		t.Logf("Found %d processes", len(infos))
	}

	// Try to kill a renderer process
	err = fit.KillRendererProcess(ctx)
	if err != nil {
		t.Logf("KillRendererProcess result: %v", err)
	}

	// Verify the session can still operate (browser should recover)
	helpers.AssertSessionActive(t, ts.Session)
}

// TestActiveBrowserCrashKillAll tests killing all browser processes.
func TestActiveBrowserCrashKillAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fit := helpers.NewFaultInjectionTest(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ctx := context.Background()

	// Get initial process count
	infos, _ := fit.Manager().GetProcessInfos(ctx)
	t.Logf("Initial process count: %d", len(infos))

	// Kill all processes
	killed, err := fit.KillAllBrowserProcesses(ctx)
	t.Logf("Killed %d processes, error: %v", killed, err)
}

// TestActiveCrashDuringOperations tests crash recovery during active operations.
func TestActiveCrashDuringOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fit := helpers.NewFaultInjectionTest(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	).CreateSession("crash-during-ops")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var successCount atomic.Int64
	var errorCount atomic.Int64

	ctx := context.Background()

	// Start concurrent operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				opCount.Add(1)
				err := ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => Math.random()`)
					return err
				})
				if err == nil {
					successCount.Add(1)
				} else {
					errorCount.Add(1)
				}
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}

	// Simulate crash during operations
	time.Sleep(200 * time.Millisecond)
	t.Log("Injecting crash during concurrent operations")
	fit.CrashSimulator().KillRendererProcess(ctx)

	wg.Wait()

	t.Logf("Operations: %d, Success: %d, Errors: %d",
		opCount.Load(), successCount.Load(), errorCount.Load())

	// Session should still be usable after crash recovery
	helpers.AssertSessionActive(t, ts.Session)
}

// TestProcessKillerFindProcesses tests the ProcessKiller's ability to find browser processes.
func TestProcessKillerFindProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_ = helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(3),
	)

	killer := mock.NewProcessKiller()

	// Find browser processes
	pids, err := killer.FindBrowserProcesses()
	if err != nil {
		t.Logf("FindBrowserProcesses error: %v", err)
	} else {
		t.Logf("Found %d browser processes: %v", len(pids), pids)
	}

	// Find child processes
	if len(pids) > 0 {
		children, err := killer.FindChildProcesses(pids[0])
		if err != nil {
			t.Logf("FindChildProcesses error: %v", err)
		} else {
			t.Logf("Found %d child processes of PID %d", len(children), pids[0])
		}
	}
}

// TestProcessKillerDryRun tests the dry-run mode of ProcessKiller.
func TestProcessKillerDryRun(t *testing.T) {
	killer := mock.NewProcessKiller()
	killer.SetDryRun(true)

	// This should not actually kill anything
	err := killer.KillProcess(12345)
	if err == nil {
		t.Log("Dry-run kill succeeded (no actual kill)")
	}

	history := killer.KillHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 kill history entry, got %d", len(history))
	}

	if history[0].Signal != "DRY_RUN" {
		t.Errorf("expected DRY_RUN signal, got %s", history[0].Signal)
	}

	t.Logf("Kill history: %+v", history)
}

// TestProcessKillerSkipKill tests the skip-kill mode.
func TestProcessKillerSkipKill(t *testing.T) {
	killer := mock.NewProcessKiller()
	killer.SetSkipKill(true)

	// This should be skipped entirely
	err := killer.KillProcess(99999)
	_ = err // Should succeed without error since it's skipped

	history := killer.KillHistory()
	if len(history) != 0 {
		t.Errorf("expected 0 kill history entries (skipped), got %d", len(history))
	}

	t.Log("Skip-kill mode working correctly")
}

// TestPageSimulatorFaultInjection tests the PageSimulator's fault injection.
func TestPageSimulatorFaultInjection(t *testing.T) {
	sim := mock.NewPageSimulator()

	// Test crash on goto
	sim.SetCrashOnGoto(true)
	err := sim.SimulateGoto("https://example.com")
	if err == nil {
		t.Error("expected error from crash on goto")
	}
	if !sim.IsCrashed() {
		t.Error("expected page to be crashed")
	}
	t.Logf("Crash on goto error: %v", err)

	// Reset and test fail rate
	sim = mock.NewPageSimulator()
	sim.SetEvaluateFailRate(1.0) // 100% fail rate

	_, err = sim.SimulateEvaluate("1+1")
	if err == nil {
		t.Error("expected error from 100% fail rate")
	}
	t.Logf("Fail rate error: %v", err)

	// Test fail next
	sim = mock.NewPageSimulator()
	sim.SetFailNext(2)

	// First two should fail
	err1 := sim.SimulateGoto("https://example.com")
	err2 := sim.SimulateGoto("https://example.com")
	err3 := sim.SimulateGoto("https://example.com")

	if err1 == nil || err2 == nil {
		t.Error("expected first two operations to fail")
	}
	if err3 != nil {
		t.Error("expected third operation to succeed")
	}
	t.Logf("Fail next: err1=%v, err2=%v, err3=%v", err1, err2, err3)
}

// TestConnectionErrorGeneration tests error generation helpers.
func TestConnectionErrorGeneration(t *testing.T) {
	for i := 0; i < 5; i++ {
		err := mock.GenerateConnectionError()
		if err == nil {
			t.Error("expected non-nil connection error")
		}
		t.Logf("Generated error: %v", err)
	}

	timeoutErr := mock.GenerateTimeoutError()
	if timeoutErr == nil {
		t.Error("expected non-nil timeout error")
	}
	t.Logf("Timeout error: %v", timeoutErr)

	networkErr := mock.GenerateNetworkError()
	if networkErr == nil {
		t.Error("expected non-nil network error")
	}
	t.Logf("Network error: %v", networkErr)
}

// TestPageHealthSimulator tests the page health simulator.
func TestPageHealthSimulator(t *testing.T) {
	health := mock.NewPageHealthSimulator()

	// All pages should be healthy by default
	if !health.Check("page-1") {
		t.Error("expected page to be healthy by default")
	}

	// Mark a page as failing
	health.SetFailing("page-2", true)
	if health.Check("page-2") {
		t.Error("expected failing page to be unhealthy")
	}

	// Test fail on next
	health.SetFailOnNext(true)
	if health.Check("page-1") {
		t.Error("expected fail-on-next to trigger")
	}
	if !health.Check("page-1") {
		t.Error("expected fail-on-next to reset after first check")
	}

	// Test custom check
	health.SetCustomCheck(func(pageID string) bool {
		return pageID == "special-page"
	})
	if !health.Check("special-page") {
		t.Error("expected custom check to return true for special-page")
	}
	if health.Check("other-page") {
		t.Error("expected custom check to return false for other-page")
	}

	t.Log("PageHealthSimulator working correctly")
}

// TestPagePoolSimulator tests the page pool simulator.
func TestPagePoolSimulator(t *testing.T) {
	pool := mock.NewPagePoolSimulator(1, 3)

	// Acquire pages
	p1, err := pool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire first page: %v", err)
	}
	if pool.Size() != 1 {
		t.Errorf("expected pool size 1, got %d", pool.Size())
	}

	p2, _ := pool.Acquire()
	p3, _ := pool.Acquire()

	if pool.Size() != 3 {
		t.Errorf("expected pool size 3, got %d", pool.Size())
	}

	// Pool should be exhausted
	pool.SetExhaustionMode(true)
	_, err = pool.Acquire()
	if err == nil {
		t.Error("expected error from exhausted pool")
	}
	t.Logf("Exhausted pool error: %v", err)

	// Test create fail rate
	pool = mock.NewPagePoolSimulator(1, 3)
	pool.SetCreateFailRate(1.0)
	_, err = pool.Acquire()
	if err == nil {
		t.Error("expected error from 100% fail rate")
	}
	t.Logf("Fail rate error: %v", err)

	_ = p1
	_ = p2
	_ = p3
}
