package browserpm

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

// BrowserManager is the top-level entry point. It owns a single browser
// instance and manages named sessions, each with its own BrowserContext
// and page pool.
type BrowserManager struct {
	config     *Config
	log        Logger
	installer  *Installer
	pw         *playwright.Playwright
	browser    playwright.Browser
	cdpSession playwright.CDPSession
	sessions   sync.Map // map[string]*Session
	closed     atomic.Bool
	closeOnce  sync.Once
	mu         sync.Mutex // protects browser/pw initialisation
}

// New creates a BrowserManager with the supplied options. It automatically
// installs the driver (if configured), launches the browser, and
// establishes a CDP session for process monitoring.
func New(opts ...Option) (*BrowserManager, error) {
	cfg := NewConfig(opts...)
	log := cfg.Logger
	if log == nil {
		log = NewZapLogger()
	}

	m := &BrowserManager{
		config: cfg,
		log:    log,
	}

	if err := m.init(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *BrowserManager) init() error {
	// Auto-install if configured.
	if m.config.Install.Auto {
		m.installer = NewInstaller(m.config, m.log)
		if err := m.installer.Install(); err != nil {
			return WrapError(err, ErrInternal, "auto-install failed")
		}
	}

	// Start Playwright.
	runOpts := &playwright.RunOptions{
		DriverDirectory: m.config.Install.Path,
	}
	pw, err := playwright.Run(runOpts)
	if err != nil {
		return WrapError(err, ErrInternal, "failed to start playwright")
	}
	m.pw = pw

	// Launch browser.
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(m.config.Browser.Headless),
		Args:     m.config.Browser.Args,
		Timeout:  playwright.Float(float64(m.config.Browser.Timeout / time.Millisecond)),
	})
	if err != nil {
		pw.Stop()
		return WrapError(err, ErrInternal, "failed to launch browser")
	}
	m.browser = browser

	// Establish browser-level CDP session for process monitoring.
	cdp, err := browser.NewBrowserCDPSession()
	if err != nil {
		m.log.Warn("CDP session unavailable (process monitoring disabled)", Err(err))
	} else {
		m.cdpSession = cdp
	}

	m.log.Info("browser manager initialised",
		String("version", browser.Version()),
		String("install_path", m.config.Install.Path))
	return nil
}

// CreateSession registers a new named session. The BrowserContext and page
// pool are created lazily on the first Do/DoShare call.
func (m *BrowserManager) CreateSession(name string, cp ContextProvider, pp PageProvider, opts ...SessionOption) (*Session, error) {
	if m.closed.Load() {
		return nil, NewError(ErrClosed, "manager is closed")
	}
	if _, loaded := m.sessions.Load(name); loaded {
		return nil, NewError(ErrSessionExists, "session already exists: "+name)
	}

	poolCfg := m.config.Pool
	for _, opt := range opts {
		opt(&poolCfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		name:            name,
		manager:         m,
		contextProvider: cp,
		pageProvider:    pp,
		poolConfig:      poolCfg,
		log:             m.log.With(String("session", name)),
		ctx:             ctx,
		cancel:          cancel,
		state:           SessionActive,
		created:         time.Now(),
	}

	if _, loaded := m.sessions.LoadOrStore(name, s); loaded {
		cancel()
		return nil, NewError(ErrSessionExists, "session already exists: "+name)
	}

	s.log.Info("session created")
	return s, nil
}

// GetSession returns an existing session by name.
func (m *BrowserManager) GetSession(name string) (*Session, error) {
	v, ok := m.sessions.Load(name)
	if !ok {
		return nil, NewError(ErrSessionNotFound, "session not found: "+name)
	}
	return v.(*Session), nil
}

// ListSessions returns a snapshot of all sessions.
func (m *BrowserManager) ListSessions() []SessionInfo {
	var infos []SessionInfo
	m.sessions.Range(func(_, value interface{}) bool {
		s := value.(*Session)
		infos = append(infos, s.Status())
		return true
	})
	return infos
}

// CloseSession shuts down and removes a session by name.
func (m *BrowserManager) CloseSession(name string) error {
	v, ok := m.sessions.LoadAndDelete(name)
	if !ok {
		return NewError(ErrSessionNotFound, "session not found: "+name)
	}
	return v.(*Session).Close()
}

// Close shuts down the entire manager: all sessions, the browser, and
// the Playwright process. Safe to call multiple times.
func (m *BrowserManager) Close() error {
	var firstErr error
	m.closeOnce.Do(func() {
		m.closed.Store(true)
		m.log.Info("shutting down browser manager")

		// Close all sessions.
		m.sessions.Range(func(key, value interface{}) bool {
			s := value.(*Session)
			if err := s.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			m.sessions.Delete(key)
			return true
		})

		// Close CDP session.
		if m.cdpSession != nil {
			m.cdpSession.Detach()
		}

		// Close browser.
		if m.browser != nil {
			if err := m.browser.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}

		// Stop Playwright.
		if m.pw != nil {
			if err := m.pw.Stop(); err != nil && firstErr == nil {
				firstErr = err
			}
		}

		m.log.Info("browser manager shut down")
		m.log.Sync()
	})
	return firstErr
}

// Browser returns the underlying playwright.Browser (for advanced use).
func (m *BrowserManager) Browser() playwright.Browser {
	return m.browser
}
