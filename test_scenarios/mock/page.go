package mock

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// PageFaultConfig configures fault injection for a page.
type PageFaultConfig struct {
	CrashOnGoto     bool
	CrashOnEvaluate bool
	CrashOnClose    bool
	CrashOnWaitFor  bool

	GotoDelay     time.Duration
	EvaluateDelay time.Duration
	WaitForDelay  time.Duration

	GotoFailRate     float64
	EvaluateFailRate float64
	WaitForFailRate  float64

	ConnectionDropOnOp bool
}

// PageSimulator simulates page behavior with controlled failure injection.
type PageSimulator struct {
	mu sync.Mutex

	closed         atomic.Bool
	crashed        atomic.Bool
	unresponsive   atomic.Bool
	scriptsBlocked atomic.Bool

	config PageFaultConfig

	navigateCount atomic.Int64
	evaluateCount atomic.Int64
	closeCount    atomic.Int64
	errorCount    atomic.Int64

	onClose       func()
	onCrash       func()
	onEvaluate    func() error
	failNextCount atomic.Int32
}

// NewPageSimulator creates a new PageSimulator.
func NewPageSimulator() *PageSimulator {
	return &PageSimulator{}
}

// SetConfig sets the fault injection configuration.
func (s *PageSimulator) SetConfig(config PageFaultConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// SetCrashOnGoto configures the page to crash on navigation.
func (s *PageSimulator) SetCrashOnGoto(crash bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.CrashOnGoto = crash
}

// SetCrashOnEvaluate configures the page to crash on JavaScript evaluation.
func (s *PageSimulator) SetCrashOnEvaluate(crash bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.CrashOnEvaluate = crash
}

// SetCrashOnClose configures the page to crash on close.
func (s *PageSimulator) SetCrashOnClose(crash bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.CrashOnClose = crash
}

// SetGotoDelay sets an artificial delay for navigation operations.
func (s *PageSimulator) SetGotoDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.GotoDelay = d
}

// SetEvaluateDelay sets an artificial delay for evaluate operations.
func (s *PageSimulator) SetEvaluateDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.EvaluateDelay = d
}

// SetGotoFailRate sets the failure rate for navigation (0.0-1.0).
func (s *PageSimulator) SetGotoFailRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.GotoFailRate = rate
}

// SetEvaluateFailRate sets the failure rate for evaluate (0.0-1.0).
func (s *PageSimulator) SetEvaluateFailRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.EvaluateFailRate = rate
}

// SetOnClose sets a callback for when the page is closed.
func (s *PageSimulator) SetOnClose(cb func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = cb
}

// SetOnCrash sets a callback for when the page crashes.
func (s *PageSimulator) SetOnCrash(cb func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onCrash = cb
}

// SetOnEvaluate sets a function to be called during evaluate, allowing dynamic error injection.
func (s *PageSimulator) SetOnEvaluate(fn func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvaluate = fn
}

// SetFailNext sets the page to fail the next N operations.
func (s *PageSimulator) SetFailNext(n int) {
	s.failNextCount.Store(int32(n))
}

// Close simulates closing the page.
func (s *PageSimulator) Close() {
	s.closed.Store(true)
	s.closeCount.Add(1)

	s.mu.Lock()
	cb := s.onClose
	s.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// Crash simulates a page crash.
func (s *PageSimulator) Crash() {
	s.crashed.Store(true)
	s.closed.Store(true)

	s.mu.Lock()
	cb := s.onCrash
	s.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// SetUnresponsive sets the page's unresponsive state.
func (s *PageSimulator) SetUnresponsive(unresponsive bool) {
	s.unresponsive.Store(unresponsive)
}

// SetScriptsBlocked sets whether JavaScript execution is blocked.
func (s *PageSimulator) SetScriptsBlocked(blocked bool) {
	s.scriptsBlocked.Store(blocked)
}

// IsClosed returns whether the page is closed.
func (s *PageSimulator) IsClosed() bool {
	return s.closed.Load()
}

// IsCrashed returns whether the page has crashed.
func (s *PageSimulator) IsCrashed() bool {
	return s.crashed.Load()
}

// IsUnresponsive returns whether the page is unresponsive.
func (s *PageSimulator) IsUnresponsive() bool {
	return s.unresponsive.Load()
}

// Stats returns page operation statistics.
func (s *PageSimulator) Stats() (navigates, evaluates, closes, errors int64) {
	return s.navigateCount.Load(), s.evaluateCount.Load(), s.closeCount.Load(), s.errorCount.Load()
}

// SimulateGoto simulates a navigation operation with fault injection.
func (s *PageSimulator) SimulateGoto(url string) error {
	s.navigateCount.Add(1)

	if s.closed.Load() {
		s.errorCount.Add(1)
		return errors.New("page has been closed")
	}

	s.mu.Lock()
	config := s.config
	cb := s.onEvaluate
	s.mu.Unlock()

	if config.CrashOnGoto {
		s.Crash()
		return errors.New("target closed: page crashed during navigation")
	}

	if config.GotoDelay > 0 {
		time.Sleep(config.GotoDelay)
	}

	if config.GotoFailRate > 0 && rand.Float64() < config.GotoFailRate {
		s.errorCount.Add(1)
		return errors.New("net::ERR_CONNECTION_RESET")
	}

	if failCount := s.failNextCount.Load(); failCount > 0 {
		s.failNextCount.Store(failCount - 1)
		s.errorCount.Add(1)
		return errors.New("net::ERR_CONNECTION_RESET")
	}

	if cb != nil {
		if err := cb(); err != nil {
			s.errorCount.Add(1)
			return err
		}
	}

	return nil
}

// SimulateEvaluate simulates a JavaScript evaluation with fault injection.
func (s *PageSimulator) SimulateEvaluate(script string) (interface{}, error) {
	s.evaluateCount.Add(1)

	if s.closed.Load() {
		s.errorCount.Add(1)
		return nil, errors.New("Execution context was destroyed")
	}

	if s.crashed.Load() {
		s.errorCount.Add(1)
		return nil, errors.New("target closed: execution context destroyed")
	}

	if s.unresponsive.Load() {
		s.errorCount.Add(1)
		return nil, errors.New("Execution context was destroyed")
	}

	if s.scriptsBlocked.Load() {
		s.errorCount.Add(1)
		return nil, errors.New("frame was detached")
	}

	s.mu.Lock()
	config := s.config
	cb := s.onEvaluate
	s.mu.Unlock()

	if config.CrashOnEvaluate {
		s.Crash()
		return nil, errors.New("Execution context was destroyed")
	}

	if config.EvaluateDelay > 0 {
		time.Sleep(config.EvaluateDelay)
	}

	if config.EvaluateFailRate > 0 && rand.Float64() < config.EvaluateFailRate {
		s.errorCount.Add(1)
		return nil, errors.New("Execution context was destroyed")
	}

	if failCount := s.failNextCount.Load(); failCount > 0 {
		s.failNextCount.Store(failCount - 1)
		s.errorCount.Add(1)
		return nil, errors.New("Execution context was destroyed")
	}

	if cb != nil {
		if err := cb(); err != nil {
			s.errorCount.Add(1)
			return nil, err
		}
	}

	return nil, nil
}

// SimulateClose simulates closing the page with fault injection.
func (s *PageSimulator) SimulateClose() error {
	s.mu.Lock()
	config := s.config
	s.mu.Unlock()

	if config.CrashOnClose {
		s.errorCount.Add(1)
		return errors.New("target closed: page crashed during close")
	}

	s.Close()
	return nil
}

// SimulateWaitFor simulates a wait operation with fault injection.
func (s *PageSimulator) SimulateWaitFor() error {
	s.mu.Lock()
	config := s.config
	s.mu.Unlock()

	if config.CrashOnWaitFor {
		return errors.New("target closed: page crashed during wait")
	}

	if config.WaitForDelay > 0 {
		time.Sleep(config.WaitForDelay)
	}

	if config.WaitForFailRate > 0 && rand.Float64() < config.WaitForFailRate {
		return errors.New("Execution context was destroyed")
	}

	return nil
}

// SimulateEvaluateError simulates a specific evaluate error type.
func (s *PageSimulator) SimulateEvaluateError(errorType string) error {
	s.errorCount.Add(1)

	errorMessages := map[string]string{
		"execution_context_destroyed": "Execution context was destroyed",
		"target_closed":               "target closed: could not read protocol padding: EOF",
		"frame_detached":              "frame was detached",
		"page_closed":                 "page has been closed",
		"connection_closed":           "Connection closed: remote hung up",
		"websocket_closed":            "websocket closed: 1006",
		"protocol_error":              "could not read protocol padding: EOF",
		"session_closed":              "Session closed unexpectedly",
	}

	if msg, ok := errorMessages[errorType]; ok {
		return errors.New(msg)
	}

	return errors.New("unknown error type: " + errorType)
}

// --- PagePoolSimulator ---

// PagePoolSimulator simulates page pool behavior with various failure scenarios.
type PagePoolSimulator struct {
	mu sync.RWMutex

	pages    []*PageSimulator
	maxPages int
	minPages int

	exhaustionMode atomic.Bool
	createFailRate float64
}

// NewPagePoolSimulator creates a new PagePoolSimulator.
func NewPagePoolSimulator(minPages, maxPages int) *PagePoolSimulator {
	return &PagePoolSimulator{
		minPages: minPages,
		maxPages: maxPages,
		pages:    make([]*PageSimulator, 0, maxPages),
	}
}

// SetExhaustionMode enables/disables pool exhaustion mode.
func (s *PagePoolSimulator) SetExhaustionMode(exhausted bool) {
	s.exhaustionMode.Store(exhausted)
}

// SetCreateFailRate sets the failure rate for page creation.
func (s *PagePoolSimulator) SetCreateFailRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createFailRate = rate
}

// Acquire simulates acquiring a page from the pool.
func (s *PagePoolSimulator) Acquire() (*PageSimulator, error) {
	s.mu.RLock()
	failRate := s.createFailRate
	s.mu.RUnlock()

	if failRate > 0 && rand.Float64() < failRate {
		return nil, errors.New("browserpm [PageUnavailable]: failed to create page")
	}

	if s.exhaustionMode.Load() {
		return nil, errors.New("browserpm [PoolExhausted]: all pages busy and pool at max capacity")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pages) >= s.maxPages {
		return nil, errors.New("browserpm [PoolExhausted]: all pages busy and pool at max capacity")
	}

	page := NewPageSimulator()
	s.pages = append(s.pages, page)
	return page, nil
}

// Release releases a page back to the pool.
func (s *PagePoolSimulator) Release(page *PageSimulator) {}

// Size returns the current pool size.
func (s *PagePoolSimulator) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pages)
}

// --- PageHealthSimulator ---

// PageHealthSimulator simulates page health scenarios.
type PageHealthSimulator struct {
	mu sync.Mutex

	healthStatus map[string]bool
	customCheck  func(pageID string) bool
	failOnNext   atomic.Bool
	failingPages map[string]bool
}

// NewPageHealthSimulator creates a new PageHealthSimulator.
func NewPageHealthSimulator() *PageHealthSimulator {
	return &PageHealthSimulator{
		healthStatus: make(map[string]bool),
		failingPages: make(map[string]bool),
	}
}

// SetHealthy sets the health status for a specific page.
func (s *PageHealthSimulator) SetHealthy(pageID string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthStatus[pageID] = healthy
}

// SetFailing marks a page as failing.
func (s *PageHealthSimulator) SetFailing(pageID string, failing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if failing {
		s.failingPages[pageID] = true
	} else {
		delete(s.failingPages, pageID)
	}
}

// SetFailOnNext sets whether the next health check should fail.
func (s *PageHealthSimulator) SetFailOnNext(fail bool) {
	s.failOnNext.Store(fail)
}

// SetCustomCheck sets a custom health check function.
func (s *PageHealthSimulator) SetCustomCheck(fn func(pageID string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customCheck = fn
}

// Check simulates a health check for a page.
func (s *PageHealthSimulator) Check(pageID string) bool {
	if s.failOnNext.Load() {
		s.failOnNext.Store(false)
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.customCheck != nil {
		return s.customCheck(pageID)
	}

	if failing, ok := s.failingPages[pageID]; ok && failing {
		return false
	}

	if status, ok := s.healthStatus[pageID]; ok {
		return status
	}

	return true
}

// Reset clears all health status settings.
func (s *PageHealthSimulator) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthStatus = make(map[string]bool)
	s.failingPages = make(map[string]bool)
	s.customCheck = nil
}

// --- Error generation helpers ---

// GenerateConnectionError generates a random connection error for testing.
func GenerateConnectionError() error {
	errs := []string{
		"target closed: could not read protocol padding: EOF",
		"Execution context was destroyed",
		"Connection closed: remote hung up",
		"websocket closed: 1006",
		"Session closed unexpectedly",
		"could not read protocol padding: invalid frame header",
		"frame was detached",
		"page has been closed",
		"socket hang up",
	}
	idx := rand.Intn(len(errs))
	return errors.New(errs[idx])
}

// GenerateTimeoutError generates a timeout error for testing.
func GenerateTimeoutError() error {
	return errors.New("context deadline exceeded")
}

// GenerateNetworkError generates a network error for testing.
func GenerateNetworkError() error {
	errs := []string{
		"net::ERR_CONNECTION_RESET",
		"net::ERR_CONNECTION_REFUSED",
		"net::ERR_CONNECTION_TIMED_OUT",
		"net::ERR_INTERNET_DISCONNECTED",
		"net::ERR_NAME_NOT_RESOLVED",
	}
	idx := rand.Intn(len(errs))
	return errors.New(errs[idx])
}
