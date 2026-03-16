package mock

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

// ConnectionSimulator simulates WebSocket/CDP connection behavior
// with controlled failure injection for testing connection resilience.
type ConnectionSimulator struct {
	mu sync.Mutex

	// Connection state
	connected     atomic.Bool
	dropAfter     time.Duration
	dropTimer     *time.Timer
	dropCount     atomic.Int64
	reconnectable atomic.Bool

	// Failure modes
	dropOnNextOp    atomic.Bool
	randomDropRate  float64 // 0.0 - 1.0
	latencyJitter   time.Duration
	connectionDelay time.Duration

	// Callbacks
	onDrop func()
}

// NewConnectionSimulator creates a new ConnectionSimulator.
func NewConnectionSimulator() *ConnectionSimulator {
	s := &ConnectionSimulator{
		reconnectable: atomic.Bool{},
	}
	s.reconnectable.Store(true)
	s.connected.Store(true)
	return s
}

// --- Configuration ---

// SetDropAfter configures the connection to drop after the specified duration.
func (s *ConnectionSimulator) SetDropAfter(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dropTimer != nil {
		s.dropTimer.Stop()
	}

	s.dropAfter = d
	s.dropTimer = time.AfterFunc(d, func() {
		s.DropConnection()
	})
}

// SetDropOnNextOp configures the connection to drop on the next operation.
func (s *ConnectionSimulator) SetDropOnNextOp(drop bool) {
	s.dropOnNextOp.Store(drop)
}

// SetRandomDropRate sets the probability (0.0-1.0) of random connection drops.
func (s *ConnectionSimulator) SetRandomDropRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.randomDropRate = rate
}

// SetLatencyJitter sets random latency variation for operations.
func (s *ConnectionSimulator) SetLatencyJitter(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latencyJitter = d
}

// SetConnectionDelay sets a fixed delay for connection operations.
func (s *ConnectionSimulator) SetConnectionDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connectionDelay = d
}

// SetReconnectable sets whether the connection can be re-established after a drop.
func (s *ConnectionSimulator) SetReconnectable(reconnectable bool) {
	s.reconnectable.Store(reconnectable)
}

// SetOnDrop sets a callback to be called when connection drops.
func (s *ConnectionSimulator) SetOnDrop(cb func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDrop = cb
}

// --- Connection Control ---

// DropConnection manually triggers a connection drop.
func (s *ConnectionSimulator) DropConnection() {
	s.connected.Store(false)
	s.dropCount.Add(1)

	s.mu.Lock()
	cb := s.onDrop
	s.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// Reconnect manually triggers a reconnection.
func (s *ConnectionSimulator) Reconnect() {
	if s.reconnectable.Load() {
		s.connected.Store(true)
	}
}

// IsConnected returns whether the connection is currently active.
func (s *ConnectionSimulator) IsConnected() bool {
	return s.connected.Load()
}

// DropCount returns the number of times the connection has been dropped.
func (s *ConnectionSimulator) DropCount() int64 {
	return s.dropCount.Load()
}

// --- Operation Wrappers ---

// WrapOperation wraps an operation with connection simulation.
// It will inject connection failures based on the simulator's configuration.
func (s *ConnectionSimulator) WrapOperation(op func() error) error {
	// Check if we should drop on this operation
	if s.dropOnNextOp.Load() {
		s.DropConnection()
		s.dropOnNextOp.Store(false)
		return errors.New("Connection closed: remote hung up")
	}

	// Check for random drops
	s.mu.Lock()
	dropRate := s.randomDropRate
	s.mu.Unlock()

	if dropRate > 0 && rand.Float64() < dropRate {
		s.DropConnection()
		return errors.New("websocket closed: 1006")
	}

	// Check if connection is already dropped
	if !s.connected.Load() {
		if s.reconnectable.Load() {
			return errors.New("target closed: could not read protocol padding: EOF")
		}
		return errors.New("Connection closed: remote hung up")
	}

	// Apply latency jitter
	s.mu.Lock()
	jitter := s.latencyJitter
	delay := s.connectionDelay
	s.mu.Unlock()

	if jitter > 0 {
		jitterAmount := time.Duration(rand.Int63n(int64(jitter*2))) - jitter
		time.Sleep(delay + jitterAmount)
	} else if delay > 0 {
		time.Sleep(delay)
	}

	return op()
}

// --- CDPSessionWrapper ---

// CDPSessionWrapper wraps a playwright.CDPSession with connection simulation.
type CDPSessionWrapper struct {
	session    playwright.CDPSession
	simulator  *ConnectionSimulator
	detached   atomic.Bool
	sendCount  atomic.Int64
	errorCount atomic.Int64
}

// NewCDPSessionWrapper creates a new CDPSessionWrapper.
func NewCDPSessionWrapper(session playwright.CDPSession, simulator *ConnectionSimulator) *CDPSessionWrapper {
	return &CDPSessionWrapper{
		session:   session,
		simulator: simulator,
	}
}

// Send sends a message through the CDP session.
func (w *CDPSessionWrapper) Send(method string, params map[string]any) (interface{}, error) {
	w.sendCount.Add(1)

	if !w.simulator.IsConnected() || w.detached.Load() {
		w.errorCount.Add(1)
		return nil, errors.New("target closed: could not read protocol padding: EOF")
	}

	var result interface{}
	err := w.simulator.WrapOperation(func() error {
		var err error
		result, err = w.session.Send(method, params)
		return err
	})
	return result, err
}

// Detach detaches the CDP session.
func (w *CDPSessionWrapper) Detach() error {
	w.detached.Store(true)
	return w.session.Detach()
}

// IsDetached returns whether the session is detached.
func (w *CDPSessionWrapper) IsDetached() bool {
	return w.detached.Load()
}

// Stats returns session statistics.
func (w *CDPSessionWrapper) Stats() (sends, errors int64) {
	return w.sendCount.Load(), w.errorCount.Load()
}

// --- ConnectionErrorGenerator ---

// ConnectionErrorGenerator generates various connection error types for testing.
type ConnectionErrorGenerator struct {
	mu sync.Mutex

	errors []ConnectionErrorType
	index  int
}

// ConnectionErrorType represents a specific connection error.
type ConnectionErrorType struct {
	Name    string
	Error   error
	IsFatal bool
}

// NewConnectionErrorGenerator creates a new ConnectionErrorGenerator.
func NewConnectionErrorGenerator() *ConnectionErrorGenerator {
	return &ConnectionErrorGenerator{
		errors: []ConnectionErrorType{
			{Name: "target_closed", Error: errors.New("target closed: could not read protocol padding: EOF"), IsFatal: true},
			{Name: "session_closed", Error: errors.New("Session closed unexpectedly"), IsFatal: true},
			{Name: "connection_closed", Error: errors.New("Connection closed: remote hung up"), IsFatal: true},
			{Name: "execution_context", Error: errors.New("Execution context was destroyed"), IsFatal: true},
			{Name: "websocket_closed", Error: errors.New("websocket closed: 1006"), IsFatal: true},
			{Name: "protocol_error", Error: errors.New("could not read protocol padding: invalid frame header"), IsFatal: true},
			{Name: "socket_hangup", Error: errors.New("socket hang up"), IsFatal: true},
			{Name: "frame_detached", Error: errors.New("frame was detached"), IsFatal: false},
		},
	}
}

// Next returns the next error in sequence.
func (g *ConnectionErrorGenerator) Next() ConnectionErrorType {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.errors) == 0 {
		return ConnectionErrorType{}
	}

	e := g.errors[g.index]
	g.index = (g.index + 1) % len(g.errors)
	return e
}

// Random returns a random connection error.
func (g *ConnectionErrorGenerator) Random() ConnectionErrorType {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.errors) == 0 {
		return ConnectionErrorType{}
	}

	idx := rand.Intn(len(g.errors))
	return g.errors[idx]
}

// ByName returns an error by its name.
func (g *ConnectionErrorGenerator) ByName(name string) (ConnectionErrorType, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, e := range g.errors {
		if e.Name == name {
			return e, true
		}
	}
	return ConnectionErrorType{}, false
}

// --- NetworkConditionSimulator ---

// NetworkConditionSimulator simulates various network conditions.
type NetworkConditionSimulator struct {
	mu sync.Mutex

	offline          atomic.Bool
	latency          time.Duration
	packetLoss       float64 // 0.0 - 1.0
	bandwidthLimit   int     // bytes per second
	jitter           time.Duration
	disruptionPeriod time.Duration
	disruptionTimer  *time.Timer
}

// NewNetworkConditionSimulator creates a new NetworkConditionSimulator.
func NewNetworkConditionSimulator() *NetworkConditionSimulator {
	return &NetworkConditionSimulator{}
}

// SetOffline sets the network offline state.
func (s *NetworkConditionSimulator) SetOffline(offline bool) {
	s.offline.Store(offline)
}

// SetLatency sets the simulated network latency.
func (s *NetworkConditionSimulator) SetLatency(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latency = d
}

// SetPacketLoss sets the packet loss rate (0.0 - 1.0).
func (s *NetworkConditionSimulator) SetPacketLoss(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packetLoss = rate
}

// SetBandwidthLimit sets the bandwidth limit in bytes per second.
func (s *NetworkConditionSimulator) SetBandwidthLimit(bytesPerSecond int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bandwidthLimit = bytesPerSecond
}

// SetJitter sets the jitter amount.
func (s *NetworkConditionSimulator) SetJitter(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jitter = d
}

// SetPeriodicDisruption sets up periodic network disruptions.
func (s *NetworkConditionSimulator) SetPeriodicDisruption(period time.Duration, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disruptionTimer != nil {
		s.disruptionTimer.Stop()
	}

	s.disruptionPeriod = period
	s.disruptionTimer = time.AfterFunc(period, func() {
		s.offline.Store(true)
		time.AfterFunc(duration, func() {
			s.offline.Store(false)
			if s.disruptionPeriod > 0 {
				s.disruptionTimer.Reset(s.disruptionPeriod)
			}
		})
	})
}

// SimulateRequest simulates a network request with configured conditions.
func (s *NetworkConditionSimulator) SimulateRequest(size int) error {
	// Check offline
	if s.offline.Load() {
		return errors.New("net::ERR_INTERNET_DISCONNECTED")
	}

	s.mu.Lock()
	latency := s.latency
	jitter := s.jitter
	packetLoss := s.packetLoss
	bandwidth := s.bandwidthLimit
	s.mu.Unlock()

	// Simulate packet loss
	if packetLoss > 0 && rand.Float64() < packetLoss {
		return errors.New("net::ERR_CONNECTION_RESET")
	}

	// Simulate latency with jitter
	if latency > 0 {
		actualLatency := latency
		if jitter > 0 {
			actualLatency += time.Duration(rand.Int63n(int64(jitter*2))) - jitter
		}
		time.Sleep(actualLatency)
	}

	// Simulate bandwidth limit
	if bandwidth > 0 && size > 0 {
		transferTime := time.Duration(float64(size)/float64(bandwidth)) * time.Second
		time.Sleep(transferTime)
	}

	return nil
}

// IsOffline returns whether the network is currently offline.
func (s *NetworkConditionSimulator) IsOffline() bool {
	return s.offline.Load()
}

// Stop stops any running timers.
func (s *NetworkConditionSimulator) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disruptionTimer != nil {
		s.disruptionTimer.Stop()
	}
}
