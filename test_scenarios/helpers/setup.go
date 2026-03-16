package helpers

import (
	"context"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
	"github.com/souloss/browserpm/test_scenarios/mock"
)

const (
	DefaultTestTimeout         = 60 * time.Second
	DefaultOperationTimeout    = 10 * time.Second
	DefaultInitTimeout         = 10 * time.Second
	DefaultHealthCheckInterval = 5 * time.Second
	DefaultPoolTTL             = 1 * time.Minute
	DefaultGracePeriod         = 5 * time.Second
)

// TestManager wraps BrowserManager with testing utilities.
type TestManager struct {
	Manager *browserpm.BrowserManager
	t       *testing.T
	cleanup []func()
}

// NewTestManager creates a new TestManager with default options.
func NewTestManager(t *testing.T, opts ...browserpm.Option) *TestManager {
	t.Helper()

	defaultOpts := []browserpm.Option{
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(DefaultPoolTTL),
		browserpm.WithOperationTimeout(DefaultOperationTimeout),
		browserpm.WithInitTimeout(DefaultInitTimeout),
		browserpm.WithHealthCheckInterval(DefaultHealthCheckInterval),
		browserpm.WithGracePeriod(DefaultGracePeriod),
	}

	allOpts := append(defaultOpts, opts...)
	manager, err := browserpm.New(allOpts...)
	if err != nil {
		t.Fatalf("failed to create test manager: %v", err)
	}

	tm := &TestManager{
		Manager: manager,
		t:       t,
	}

	t.Cleanup(tm.Close)
	return tm
}

func (tm *TestManager) Close() {
	for i := len(tm.cleanup) - 1; i >= 0; i-- {
		tm.cleanup[i]()
	}
	if tm.Manager != nil {
		tm.Manager.Close()
	}
}

func (tm *TestManager) AddCleanup(fn func()) {
	tm.cleanup = append(tm.cleanup, fn)
}

type TestSession struct {
	Session *browserpm.Session
	tm      *TestManager
	t       *testing.T
}

func (tm *TestManager) CreateSession(name string, opts ...browserpm.SessionOption) *TestSession {
	tm.t.Helper()

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-test/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			return nil
		},
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			return nil
		},
		func(ctx context.Context, page playwright.Page) bool {
			return !page.IsClosed()
		},
	)

	session, err := tm.Manager.CreateSession(name, cp, pp, opts...)
	if err != nil {
		tm.t.Fatalf("failed to create test session: %v", err)
	}

	ts := &TestSession{
		Session: session,
		tm:      tm,
		t:       tm.t,
	}

	tm.AddCleanup(func() {
		session.Close()
	})

	return ts
}

func (tm *TestManager) CreateSessionWithNavigation(name string, targetURL string, opts ...browserpm.SessionOption) *TestSession {
	tm.t.Helper()

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-test/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			return nil
		},
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			_, err := page.Goto(targetURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			})
			return err
		},
		func(ctx context.Context, page playwright.Page) bool {
			return !page.IsClosed()
		},
	)

	session, err := tm.Manager.CreateSession(name, cp, pp, opts...)
	if err != nil {
		tm.t.Fatalf("failed to create test session: %v", err)
	}

	ts := &TestSession{
		Session: session,
		tm:      tm,
		t:       tm.t,
	}

	tm.AddCleanup(func() {
		session.Close()
	})

	return ts
}

func (ts *TestSession) Do(op func(page playwright.Page) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultOperationTimeout)
	defer cancel()
	return ts.Session.Do(ctx, op)
}

func (ts *TestSession) DoShare(op func(page playwright.Page) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultOperationTimeout)
	defer cancel()
	return ts.Session.DoShare(ctx, op)
}

type ContextOption func(*TestConfig)

type TestConfig struct {
	MinPages            int
	MaxPages            int
	PoolTTL             time.Duration
	OperationTimeout    time.Duration
	InitTimeout         time.Duration
	HealthCheckInterval time.Duration
	GracePeriod         time.Duration
	Headless            bool
}

func WithMinPages(n int) ContextOption {
	return func(c *TestConfig) {
		c.MinPages = n
	}
}

func WithMaxPages(n int) ContextOption {
	return func(c *TestConfig) {
		c.MaxPages = n
	}
}

func WithPoolTTL(d time.Duration) ContextOption {
	return func(c *TestConfig) {
		c.PoolTTL = d
	}
}

func WithOperationTimeout(d time.Duration) ContextOption {
	return func(c *TestConfig) {
		c.OperationTimeout = d
	}
}

func NewTestConfig(opts ...ContextOption) *TestConfig {
	config := &TestConfig{
		MinPages:            1,
		MaxPages:            5,
		PoolTTL:             DefaultPoolTTL,
		OperationTimeout:    DefaultOperationTimeout,
		InitTimeout:         DefaultInitTimeout,
		HealthCheckInterval: DefaultHealthCheckInterval,
		GracePeriod:         DefaultGracePeriod,
		Headless:            true,
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

func (c *TestConfig) ToOptions() []browserpm.Option {
	return []browserpm.Option{
		browserpm.WithHeadless(c.Headless),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(c.MinPages),
		browserpm.WithMaxPages(c.MaxPages),
		browserpm.WithPoolTTL(c.PoolTTL),
		browserpm.WithOperationTimeout(c.OperationTimeout),
		browserpm.WithInitTimeout(c.InitTimeout),
		browserpm.WithHealthCheckInterval(c.HealthCheckInterval),
		browserpm.WithGracePeriod(c.GracePeriod),
	}
}

func WithContextOptions(config *TestConfig) []browserpm.Option {
	return config.ToOptions()
}

type RetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
	Timeout     time.Duration
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts: 3,
		Delay:       100 * time.Millisecond,
		Timeout:     DefaultTestTimeout,
	}
}

func Retry(t *testing.T, config *RetryConfig, fn func() error) error {
	t.Helper()

	var lastErr error
	for i := 0; i < config.MaxAttempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		t.Logf("Attempt %d/%d failed: %v", i+1, config.MaxAttempts, err)
		time.Sleep(config.Delay)
	}
	return lastErr
}

// --- Fault Injection Helpers ---

// FaultInjectionTest wraps test utilities for fault injection testing.
type FaultInjectionTest struct {
	t          *testing.T
	manager    *browserpm.BrowserManager
	killer     *mock.ProcessKiller
	crashSim   *mock.BrowserCrashSimulator
	setupCount int
}

// NewFaultInjectionTest creates a new fault injection test helper.
func NewFaultInjectionTest(t *testing.T, opts ...browserpm.Option) *FaultInjectionTest {
	t.Helper()

	tm := NewTestManager(t, opts...)
	killer := mock.NewProcessKiller()

	crashSim := mock.NewBrowserCrashSimulator(func(ctx context.Context) ([]mock.ProcessInfo, error) {
		infos, err := tm.Manager.GetProcessInfos(ctx)
		if err != nil {
			return nil, err
		}
		mockInfos := make([]mock.ProcessInfo, len(infos))
		for i, info := range infos {
			mockInfos[i] = mock.ProcessInfo{
				ID:              info.ID,
				Type:            info.Type,
				CPU:             info.CPU,
				RSS:             info.RSS,
				VMS:             info.VMS,
				ExclusiveMemory: info.ExclusiveMemory,
			}
		}
		return mockInfos, nil
	})

	return &FaultInjectionTest{
		t:        t,
		manager:  tm.Manager,
		killer:   killer,
		crashSim: crashSim,
	}
}

// Manager returns the underlying BrowserManager.
func (f *FaultInjectionTest) Manager() *browserpm.BrowserManager {
	return f.manager
}

// Killer returns the ProcessKiller for manual process manipulation.
func (f *FaultInjectionTest) Killer() *mock.ProcessKiller {
	return f.killer
}

// CrashSimulator returns the BrowserCrashSimulator.
func (f *FaultInjectionTest) CrashSimulator() *mock.BrowserCrashSimulator {
	return f.crashSim
}

// KillBrowser kills all browser processes found on the system.
func (f *FaultInjectionTest) KillBrowser() error {
	f.t.Helper()
	killed, errs := f.killer.KillBrowser()
	if len(errs) > 0 {
		f.t.Logf("KillBrowser: killed %d processes, errors: %v", killed, errs)
		return errs[0]
	}
	f.t.Logf("KillBrowser: killed %d processes", killed)
	return nil
}

// KillBrowserProcess kills a specific browser process by PID.
func (f *FaultInjectionTest) KillBrowserProcess(pid int32) error {
	f.t.Helper()
	return f.killer.KillProcess(pid)
}

// KillMainBrowserProcess kills the main browser process via CDP.
func (f *FaultInjectionTest) KillMainBrowserProcess(ctx context.Context) error {
	f.t.Helper()
	return f.crashSim.KillMainProcess(ctx)
}

// KillAllBrowserProcesses kills all browser processes via CDP.
func (f *FaultInjectionTest) KillAllBrowserProcesses(ctx context.Context) (int, error) {
	f.t.Helper()
	return f.crashSim.KillAllProcesses(ctx)
}

// KillRendererProcess kills a renderer process to simulate page crash.
func (f *FaultInjectionTest) KillRendererProcess(ctx context.Context) error {
	f.t.Helper()
	return f.crashSim.KillRendererProcess(ctx)
}

// SetupCrashOnOp sets up a session to crash after N operations.
func (f *FaultInjectionTest) SetupCrashOnOp(session *browserpm.Session, afterOps int) {
	f.t.Helper()
	f.setupCount = afterOps
}

// CreateSessionWithFaultInjection creates a session that can simulate faults.
func (f *FaultInjectionTest) CreateSessionWithFaultInjection(name string, injectFault func() error, opts ...browserpm.SessionOption) (*browserpm.Session, error) {
	f.t.Helper()

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-fault-test/1.0"),
		},
		nil,
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			if injectFault != nil {
				return injectFault()
			}
			return nil
		},
		func(ctx context.Context, page playwright.Page) bool {
			return !page.IsClosed()
		},
	)

	return f.manager.CreateSession(name, cp, pp, opts...)
}

// InjectConnectionError injects a connection error during page operations.
func (f *FaultInjectionTest) InjectConnectionError() error {
	return mock.GenerateConnectionError()
}

// InjectTimeoutError injects a timeout error.
func (f *FaultInjectionTest) InjectTimeoutError() error {
	return mock.GenerateTimeoutError()
}

// InjectNetworkError injects a network error.
func (f *FaultInjectionTest) InjectNetworkError() error {
	return mock.GenerateNetworkError()
}

// AssertProcessKilled asserts that a process was killed.
func (f *FaultInjectionTest) AssertProcessKilled(pid int32) {
	f.t.Helper()
	history := f.killer.KillHistory()
	for _, record := range history {
		if record.PID == pid && record.Success {
			return
		}
	}
	f.t.Errorf("expected process %d to be killed, but it was not", pid)
}

// AssertProcessCountKilled asserts that at least N processes were killed.
func (f *FaultInjectionTest) AssertProcessCountKilled(minCount int) {
	f.t.Helper()
	pids := f.killer.KilledPIDs()
	if len(pids) < minCount {
		f.t.Errorf("expected at least %d processes killed, got %d", minCount, len(pids))
	}
}

// --- Scenario Helpers ---

// RunCrashScenario runs a crash scenario with automatic verification.
func (f *FaultInjectionTest) RunCrashScenario(ctx context.Context, session *browserpm.Session, opsBeforeCrash int, expectedSuccess int) {
	f.t.Helper()

	var successCount, errorCount int

	for i := 0; i < opsBeforeCrash+5; i++ {
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := page.Evaluate(`() => 1`)
			return err
		})

		if i == opsBeforeCrash {
			f.t.Logf("Injecting crash at operation %d", i)
			f.crashSim.KillRendererProcess(ctx)
		}

		if err == nil {
			successCount++
		} else {
			errorCount++
			f.t.Logf("Operation %d error: %v", i, err)
		}
	}

	f.t.Logf("Crash scenario: success=%d, errors=%d", successCount, errorCount)
}

// RunConnectionDropScenario simulates connection drops during operations.
func (f *FaultInjectionTest) RunConnectionDropScenario(ctx context.Context, session *browserpm.Session, dropAfter int) {
	f.t.Helper()

	var successCount, errorCount int

	for i := 0; i < dropAfter+10; i++ {
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := page.Evaluate(`() => Date.now()`)
			return err
		})

		if i == dropAfter {
			f.t.Logf("Simulating connection drop at operation %d", i)
		}

		if err == nil {
			successCount++
		} else {
			errorCount++
			if browserpm.IsConnectionError(err) {
				f.t.Logf("Connection error detected at operation %d: %v", i, err)
			}
		}
	}

	f.t.Logf("Connection drop scenario: success=%d, errors=%d", successCount, errorCount)
}
