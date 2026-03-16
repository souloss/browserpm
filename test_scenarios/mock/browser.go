// Package mock provides mock implementations for testing browserpm
// in various failure scenarios.
package mock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/shirou/gopsutil/v3/process"
)

// BrowserSimulator simulates browser behavior with controlled failure injection.
// It wraps a real playwright.Browser and can simulate various failure modes.
type BrowserSimulator struct {
	browser playwright.Browser
	mu      sync.Mutex

	// Failure injection controls
	crashed        atomic.Bool
	crashOnNewCtx  atomic.Bool
	crashOnNewPage atomic.Bool
	crashAfter     time.Duration // Crash after this duration
	crashTimer     *time.Timer

	// Behavior controls
	newContextDelay time.Duration
	newPageDelay    time.Duration
	crashCallback   func()

	// Statistics
	newContextCalls atomic.Int64
	newPageCalls    atomic.Int64
	crashCount      atomic.Int64
}

// NewBrowserSimulator creates a new BrowserSimulator wrapping the given browser.
func NewBrowserSimulator(browser playwright.Browser) *BrowserSimulator {
	return &BrowserSimulator{
		browser: browser,
	}
}

// --- Simulation Controls ---

// SetCrashOnNewContext configures the simulator to crash when NewContext is called.
func (s *BrowserSimulator) SetCrashOnNewContext(crash bool) {
	s.crashOnNewCtx.Store(crash)
}

// SetCrashOnNewPage configures the simulator to crash when NewPage is called.
func (s *BrowserSimulator) SetCrashOnNewPage(crash bool) {
	s.crashOnNewPage.Store(crash)
}

// SetCrashAfter configures the simulator to crash after the specified duration.
// This is useful for simulating intermittent crashes during operations.
func (s *BrowserSimulator) SetCrashAfter(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.crashTimer != nil {
		s.crashTimer.Stop()
	}

	s.crashAfter = d
	s.crashTimer = time.AfterFunc(d, func() {
		s.TriggerCrash()
	})
}

// SetNewContextDelay sets an artificial delay for NewContext calls.
func (s *BrowserSimulator) SetNewContextDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newContextDelay = d
}

// SetNewPageDelay sets an artificial delay for NewPage calls.
func (s *BrowserSimulator) SetNewPageDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newPageDelay = d
}

// SetCrashCallback sets a callback to be invoked when a crash is triggered.
func (s *BrowserSimulator) SetCrashCallback(cb func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crashCallback = cb
}

// TriggerCrash manually triggers a simulated browser crash.
func (s *BrowserSimulator) TriggerCrash() {
	s.crashed.Store(true)
	s.crashCount.Add(1)

	s.mu.Lock()
	cb := s.crashCallback
	s.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// Recover clears the crashed state, allowing the browser to be "restarted".
func (s *BrowserSimulator) Recover() {
	s.crashed.Store(false)
}

// IsCrashed returns whether the browser is currently in a crashed state.
func (s *BrowserSimulator) IsCrashed() bool {
	return s.crashed.Load()
}

// Stats returns statistics about the simulator's operations.
func (s *BrowserSimulator) Stats() (newContextCalls, newPageCalls, crashCount int64) {
	return s.newContextCalls.Load(), s.newPageCalls.Load(), s.crashCount.Load()
}

// --- Browser Interface Implementation ---

// NewContext creates a new browser context, with optional failure injection.
func (s *BrowserSimulator) NewContext(options ...playwright.BrowserNewContextOptions) (playwright.BrowserContext, error) {
	s.newContextCalls.Add(1)

	// Simulate delay
	s.mu.Lock()
	delay := s.newContextDelay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	// Simulate crash
	if s.crashOnNewCtx.Load() || s.crashed.Load() {
		return nil, errors.New("target closed: browser crashed")
	}

	return s.browser.NewContext(options...)
}

// NewPage creates a new page, with optional failure injection.
func (s *BrowserSimulator) NewPage(options ...playwright.BrowserNewPageOptions) (playwright.Page, error) {
	s.newPageCalls.Add(1)

	// Simulate delay
	s.mu.Lock()
	delay := s.newPageDelay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	// Simulate crash
	if s.crashOnNewPage.Load() || s.crashed.Load() {
		return nil, errors.New("target closed: browser crashed")
	}

	return s.browser.NewPage(options...)
}

// Close closes the browser.
func (s *BrowserSimulator) Close() error {
	s.mu.Lock()
	if s.crashTimer != nil {
		s.crashTimer.Stop()
	}
	s.mu.Unlock()
	return s.browser.Close()
}

// Version returns the browser version.
func (s *BrowserSimulator) Version() string {
	if s.crashed.Load() {
		return ""
	}
	return s.browser.Version()
}

// IsConnected returns whether the browser is connected.
func (s *BrowserSimulator) IsConnected() bool {
	return !s.crashed.Load()
}

// NewBrowserCDPSession creates a new browser-level CDP session.
func (s *BrowserSimulator) NewBrowserCDPSession() (playwright.CDPSession, error) {
	if s.crashed.Load() {
		return nil, errors.New("target closed: browser crashed")
	}
	return s.browser.NewBrowserCDPSession()
}

// Contexts returns all browser contexts.
func (s *BrowserSimulator) Contexts() []playwright.BrowserContext {
	if s.crashed.Load() {
		return nil
	}
	return s.browser.Contexts()
}

// --- CrashInjector ---

// CrashInjector provides methods to inject various crash scenarios.
type CrashInjector struct {
	mu sync.Mutex

	// Crash patterns
	crashPatterns []CrashPattern
	activePattern int
}

// CrashPattern defines a crash scenario.
type CrashPattern struct {
	Name        string
	Description string
	Inject      func() error
}

// NewCrashInjector creates a new CrashInjector.
func NewCrashInjector() *CrashInjector {
	return &CrashInjector{
		crashPatterns: []CrashPattern{
			{
				Name:        "target_closed",
				Description: "Simulates 'target closed' error",
				Inject: func() error {
					return errors.New("target closed: could not read protocol padding: EOF")
				},
			},
			{
				Name:        "execution_context_destroyed",
				Description: "Simulates execution context destroyed error",
				Inject: func() error {
					return errors.New("Execution context was destroyed")
				},
			},
			{
				Name:        "connection_closed",
				Description: "Simulates connection closed error",
				Inject: func() error {
					return errors.New("Connection closed: remote hung up")
				},
			},
			{
				Name:        "websocket_closed",
				Description: "Simulates WebSocket closed error",
				Inject: func() error {
					return errors.New("websocket closed: 1006")
				},
			},
			{
				Name:        "session_closed",
				Description: "Simulates session closed error",
				Inject: func() error {
					return errors.New("Session closed unexpectedly")
				},
			},
			{
				Name:        "protocol_error",
				Description: "Simulates CDP protocol error",
				Inject: func() error {
					return errors.New("could not read protocol padding: EOF")
				},
			},
			{
				Name:        "frame_detached",
				Description: "Simulates frame detached error",
				Inject: func() error {
					return errors.New("frame was detached")
				},
			},
			{
				Name:        "page_closed",
				Description: "Simulates page already closed error",
				Inject: func() error {
					return errors.New("page has been closed")
				},
			},
		},
	}
}

// Patterns returns all available crash patterns.
func (ci *CrashInjector) Patterns() []CrashPattern {
	return ci.crashPatterns
}

// Inject injects a specific crash pattern by name.
func (ci *CrashInjector) Inject(name string) error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	for _, p := range ci.crashPatterns {
		if p.Name == name {
			return p.Inject()
		}
	}
	return errors.New("unknown crash pattern: " + name)
}

// InjectRandom injects a random crash pattern.
func (ci *CrashInjector) InjectRandom() error {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if len(ci.crashPatterns) == 0 {
		return errors.New("no crash patterns available")
	}

	idx := int(time.Now().UnixNano()) % len(ci.crashPatterns)
	return ci.crashPatterns[idx].Inject()
}

// --- ProcessKiller ---

// ProcessKiller provides utilities to kill browser processes for testing.
// It can find and kill Chromium processes by PID, supporting both graceful
// shutdown (SIGTERM) and forceful termination (SIGKILL).
type ProcessKiller struct {
	mu sync.Mutex

	killedPIDs  []int32
	killHistory []KillRecord
	dryRun      bool // if true, don't actually kill (for testing the killer itself)
	skipKill    atomic.Bool
	killSignal  syscall.Signal
	gracePeriod time.Duration
}

// KillRecord records a kill operation.
type KillRecord struct {
	PID       int32
	Signal    string
	Timestamp time.Time
	Success   bool
	Error     error
}

// NewProcessKiller creates a new ProcessKiller.
func NewProcessKiller() *ProcessKiller {
	return &ProcessKiller{
		killSignal:  syscall.SIGKILL,
		gracePeriod: 0,
	}
}

// SetDryRun sets whether to actually kill processes (for testing).
func (pk *ProcessKiller) SetDryRun(dryRun bool) {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	pk.dryRun = dryRun
}

// SetSkipKill sets whether to skip kill operations entirely.
func (pk *ProcessKiller) SetSkipKill(skip bool) {
	pk.skipKill.Store(skip)
}

// SetKillSignal sets the signal to use for killing (SIGTERM or SIGKILL).
func (pk *ProcessKiller) SetKillSignal(sig syscall.Signal) {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	pk.killSignal = sig
}

// SetGracePeriod sets a grace period before forceful termination.
func (pk *ProcessKiller) SetGracePeriod(d time.Duration) {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	pk.gracePeriod = d
}

// FindBrowserProcesses finds all Chromium browser processes.
// It searches by process name and returns matching PIDs.
func (pk *ProcessKiller) FindBrowserProcesses() ([]int32, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	var browserPIDs []int32
	browserNames := []string{"chromium", "chrome", "Chromium", "Chrome", "chromium-browser", "google-chrome"}

	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			continue
		}

		for _, browserName := range browserNames {
			if name == browserName || containsSubstring(name, browserName) {
				browserPIDs = append(browserPIDs, p.Pid)
				break
			}
		}
	}

	return browserPIDs, nil
}

// FindProcessesByPort finds browser processes that might be using specific debugging ports.
// Chromium processes started by Playwright typically have --remote-debugging-port in their cmdline.
func (pk *ProcessKiller) FindProcessesByPort(port int) ([]int32, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	var matchingPIDs []int32
	portArg := fmt.Sprintf("--remote-debugging-port=%d", port)

	for _, p := range processes {
		cmdline, err := p.Cmdline()
		if err != nil {
			continue
		}

		if containsSubstring(cmdline, portArg) || containsSubstring(cmdline, "--remote-debugging-port") {
			matchingPIDs = append(matchingPIDs, p.Pid)
		}
	}

	return matchingPIDs, nil
}

// FindChildProcesses finds all child processes of a given PID.
func (pk *ProcessKiller) FindChildProcesses(parentPID int32) ([]int32, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	var childPIDs []int32
	for _, p := range processes {
		ppid, err := p.Ppid()
		if err != nil {
			continue
		}
		if ppid == parentPID {
			childPIDs = append(childPIDs, p.Pid)
		}
	}

	return childPIDs, nil
}

// KillProcess kills a specific process by PID.
// Returns an error if the process doesn't exist or can't be killed.
func (pk *ProcessKiller) KillProcess(pid int32) error {
	if pk.skipKill.Load() {
		return nil
	}

	pk.mu.Lock()
	dryRun := pk.dryRun
	sig := pk.killSignal
	grace := pk.gracePeriod
	pk.mu.Unlock()

	record := KillRecord{
		PID:       pid,
		Timestamp: time.Now(),
	}

	defer func() {
		pk.mu.Lock()
		pk.killHistory = append(pk.killHistory, record)
		pk.killedPIDs = append(pk.killedPIDs, pid)
		pk.mu.Unlock()
	}()

	if dryRun {
		record.Signal = "DRY_RUN"
		record.Success = true
		return nil
	}

	// Check if process exists
	exists, err := process.PidExists(pid)
	if err != nil {
		record.Error = err
		return fmt.Errorf("failed to check if process %d exists: %w", pid, err)
	}
	if !exists {
		record.Error = fmt.Errorf("process %d does not exist", pid)
		return record.Error
	}

	// Find the process
	p, err := process.NewProcess(pid)
	if err != nil {
		record.Error = err
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Determine signal name
	signalName := "SIGKILL"
	if sig == syscall.SIGTERM {
		signalName = "SIGTERM"
	}
	record.Signal = signalName

	// Kill the process
	if err := p.Kill(); err != nil {
		record.Error = err
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	// Wait for process to actually terminate
	if grace > 0 {
		time.Sleep(grace)
	}

	// Verify it's dead
	// gopsutil's Kill() sends SIGKILL on Unix systems
	// We need to wait a bit and verify
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exists, _ = process.PidExists(pid)
		if !exists {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	record.Success = true
	return nil
}

// KillProcessForce forcefully kills a process using SIGKILL (or taskkill on Windows).
func (pk *ProcessKiller) KillProcessForce(pid int32) error {
	if pk.skipKill.Load() {
		return nil
	}

	pk.mu.Lock()
	dryRun := pk.dryRun
	pk.mu.Unlock()

	record := KillRecord{
		PID:       pid,
		Signal:    "SIGKILL_FORCE",
		Timestamp: time.Now(),
	}

	defer func() {
		pk.mu.Lock()
		pk.killHistory = append(pk.killHistory, record)
		pk.killedPIDs = append(pk.killedPIDs, pid)
		pk.mu.Unlock()
	}()

	if dryRun {
		record.Success = true
		return nil
	}

	// Platform-specific force kill
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
		if err := cmd.Run(); err != nil {
			record.Error = err
			return fmt.Errorf("taskkill failed for PID %d: %w", pid, err)
		}
	} else {
		// Unix: send SIGKILL directly
		process, err := os.FindProcess(int(pid))
		if err != nil {
			record.Error = err
			return fmt.Errorf("failed to find process %d: %w", pid, err)
		}
		if err := process.Signal(syscall.SIGKILL); err != nil {
			record.Error = err
			return fmt.Errorf("failed to send SIGKILL to PID %d: %w", pid, err)
		}
	}

	record.Success = true
	return nil
}

// KillBrowser kills all browser processes found on the system.
// Returns the number of processes killed and any errors.
func (pk *ProcessKiller) KillBrowser() (killed int, errs []error) {
	pids, err := pk.FindBrowserProcesses()
	if err != nil {
		return 0, []error{err}
	}

	for _, pid := range pids {
		if err := pk.KillProcess(pid); err != nil {
			errs = append(errs, err)
		} else {
			killed++
		}
	}

	return killed, errs
}

// KillBrowserTree kills a browser process and all its children.
// This is useful for ensuring complete cleanup.
func (pk *ProcessKiller) KillBrowserTree(mainPID int32) error {
	// First, find all child processes
	children, err := pk.FindChildProcesses(mainPID)
	if err != nil {
		return fmt.Errorf("failed to find child processes: %w", err)
	}

	// Kill children first (bottom-up)
	var killErrors []error
	for _, childPID := range children {
		// Recursively kill grandchildren
		if err := pk.KillBrowserTree(childPID); err != nil {
			killErrors = append(killErrors, err)
		}
	}

	// Now kill the parent
	if err := pk.KillProcess(mainPID); err != nil {
		killErrors = append(killErrors, err)
	}

	if len(killErrors) > 0 {
		return fmt.Errorf("errors during kill tree: %v", killErrors)
	}
	return nil
}

// KilledPIDs returns the list of killed process IDs.
func (pk *ProcessKiller) KilledPIDs() []int32 {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	result := make([]int32, len(pk.killedPIDs))
	copy(result, pk.killedPIDs)
	return result
}

// KillHistory returns the history of kill operations.
func (pk *ProcessKiller) KillHistory() []KillRecord {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	result := make([]KillRecord, len(pk.killHistory))
	copy(result, pk.killHistory)
	return result
}

// Reset clears the killed process list and history.
func (pk *ProcessKiller) Reset() {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	pk.killedPIDs = nil
	pk.killHistory = nil
}

// --- BrowserCrashSimulator ---

// BrowserCrashSimulator combines ProcessKiller with a BrowserManager to provide
// realistic browser crash simulation for testing.
type BrowserCrashSimulator struct {
	getProcessInfos func(ctx context.Context) ([]ProcessInfo, error)
	killer          *ProcessKiller
	cdpGetter       interface {
		NewBrowserCDPSession() (playwright.CDPSession, error)
	}
	mu      sync.Mutex
	crashed atomic.Bool
}

// ProcessInfo represents process information (compatible with browserpm package).
type ProcessInfo struct {
	ID              int32
	Type            string
	CPU             float64
	RSS             uint64
	VMS             uint64
	ExclusiveMemory uint64
}

// NewBrowserCrashSimulator creates a new crash simulator.
func NewBrowserCrashSimulator(getProcessInfos func(ctx context.Context) ([]ProcessInfo, error)) *BrowserCrashSimulator {
	return &BrowserCrashSimulator{
		getProcessInfos: getProcessInfos,
		killer:          NewProcessKiller(),
	}
}

// NewBrowserCrashSimulatorFromManager creates a crash simulator from a BrowserManager.
// This is a convenience function that handles the type conversion.
func NewBrowserCrashSimulatorFromManager(getPIDs func(ctx context.Context) ([]int32, map[int32]string, error)) *BrowserCrashSimulator {
	return &BrowserCrashSimulator{
		getProcessInfos: func(ctx context.Context) ([]ProcessInfo, error) {
			pids, types, err := getPIDs(ctx)
			if err != nil {
				return nil, err
			}
			infos := make([]ProcessInfo, len(pids))
			for i, pid := range pids {
				infos[i] = ProcessInfo{ID: pid}
				if t, ok := types[pid]; ok {
					infos[i].Type = t
				}
			}
			return infos, nil
		},
		killer: NewProcessKiller(),
	}
}

// SetCDPGetter sets the CDP session getter for advanced crash simulation.
func (s *BrowserCrashSimulator) SetCDPGetter(getter interface {
	NewBrowserCDPSession() (playwright.CDPSession, error)
}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cdpGetter = getter
}

// KillMainProcess kills the main browser process.
func (s *BrowserCrashSimulator) KillMainProcess(ctx context.Context) error {
	infos, err := s.getProcessInfos(ctx)
	if err != nil {
		return fmt.Errorf("failed to get process infos: %w", err)
	}

	// Find the browser process (usually the first one or the one with type "browser")
	var browserPID int32 = -1
	for _, info := range infos {
		if info.Type == "browser" {
			browserPID = info.ID
			break
		}
	}
	if browserPID == -1 && len(infos) > 0 {
		browserPID = infos[0].ID
	}

	if browserPID == -1 {
		return errors.New("no browser process found")
	}

	s.crashed.Store(true)
	return s.killer.KillProcess(browserPID)
}

// KillAllProcesses kills all browser-related processes.
func (s *BrowserCrashSimulator) KillAllProcesses(ctx context.Context) (int, error) {
	infos, err := s.getProcessInfos(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get process infos: %w", err)
	}

	var killed int
	var lastErr error
	for _, info := range infos {
		if err := s.killer.KillProcess(info.ID); err != nil {
			lastErr = err
		} else {
			killed++
		}
	}

	s.crashed.Store(true)
	return killed, lastErr
}

// KillRendererProcess kills a renderer process.
func (s *BrowserCrashSimulator) KillRendererProcess(ctx context.Context) error {
	infos, err := s.getProcessInfos(ctx)
	if err != nil {
		return fmt.Errorf("failed to get process infos: %w", err)
	}

	for _, info := range infos {
		if info.Type == "renderer" || info.Type == "tab" {
			return s.killer.KillProcess(info.ID)
		}
	}

	return errors.New("no renderer process found")
}

// KillGPUProcess kills the GPU process.
func (s *BrowserCrashSimulator) KillGPUProcess(ctx context.Context) error {
	infos, err := s.getProcessInfos(ctx)
	if err != nil {
		return fmt.Errorf("failed to get process infos: %w", err)
	}

	for _, info := range infos {
		if info.Type == "gpu" {
			return s.killer.KillProcess(info.ID)
		}
	}

	return errors.New("no GPU process found")
}

// SimulateConnectionDrop simulates a connection drop by closing the CDP session.
func (s *BrowserCrashSimulator) SimulateConnectionDrop() error {
	s.mu.Lock()
	cdp := s.cdpGetter
	s.mu.Unlock()

	if cdp == nil {
		return errors.New("CDP getter not set")
	}

	session, err := cdp.NewBrowserCDPSession()
	if err != nil {
		return fmt.Errorf("failed to create CDP session: %w", err)
	}

	// Detach the session to simulate connection drop
	if err := session.Detach(); err != nil {
		return fmt.Errorf("failed to detach CDP session: %w", err)
	}

	return nil
}

// IsCrashed returns whether a crash has been simulated.
func (s *BrowserCrashSimulator) IsCrashed() bool {
	return s.crashed.Load()
}

// Reset clears the crashed state.
func (s *BrowserCrashSimulator) Reset() {
	s.crashed.Store(false)
	s.killer.Reset()
}

// GetKiller returns the underlying ProcessKiller for advanced operations.
func (s *BrowserCrashSimulator) GetKiller() *ProcessKiller {
	return s.killer
}

// Helper function
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
