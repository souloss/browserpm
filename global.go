package browserpm

import (
	"context"
	"sync"
)

var (
	globalManager *BrowserManager
	globalOnce    sync.Once
	globalOpts    []Option
	globalMu      sync.Mutex
	globalInitErr error
)

// SetGlobalOptions configures options for the global singleton. Must be
// called before the first call to Global() / MustGlobal(). Calls after
// initialisation are silently ignored.
func SetGlobalOptions(opts ...Option) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager == nil {
		globalOpts = append(globalOpts, opts...)
	}
}

// Global returns the lazily-initialised global BrowserManager singleton.
func Global() (*BrowserManager, error) {
	globalOnce.Do(func() {
		globalMu.Lock()
		opts := globalOpts
		globalMu.Unlock()
		globalManager, globalInitErr = New(opts...)
	})
	return globalManager, globalInitErr
}

// MustGlobal is like Global but panics on error.
func MustGlobal() *BrowserManager {
	m, err := Global()
	if err != nil {
		panic("browserpm: " + err.Error())
	}
	return m
}

// --- Convenience wrappers that delegate to the global singleton ---

// GCreateSession creates a session on the global manager.
func GCreateSession(name string, cp ContextProvider, pp PageProvider, opts ...SessionOption) (*Session, error) {
	m, err := Global()
	if err != nil {
		return nil, err
	}
	return m.CreateSession(name, cp, pp, opts...)
}

// GGetSession retrieves a session from the global manager.
func GGetSession(name string) (*Session, error) {
	m, err := Global()
	if err != nil {
		return nil, err
	}
	return m.GetSession(name)
}

// GCloseSession closes a session on the global manager.
func GCloseSession(name string) error {
	m, err := Global()
	if err != nil {
		return err
	}
	return m.CloseSession(name)
}

// GListSessions lists sessions on the global manager.
func GListSessions() ([]SessionInfo, error) {
	m, err := Global()
	if err != nil {
		return nil, err
	}
	return m.ListSessions(), nil
}

// GGetProcessInfos retrieves process info from the global manager.
func GGetProcessInfos(ctx context.Context) ([]ProcessInfo, error) {
	m, err := Global()
	if err != nil {
		return nil, err
	}
	return m.GetProcessInfos(ctx)
}

// Shutdown closes the global singleton. After calling this the global
// manager is no longer usable and Global() will return the cached error.
func Shutdown() error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager != nil {
		err := globalManager.Close()
		globalManager = nil
		return err
	}
	return nil
}
