package browserpm

import (
	"time"
)

const defaultInstallPath = "./playwright-driver"

// BrowserConfig contains browser startup options.
type BrowserConfig struct {
	Headless bool
	Args     []string
	Timeout  time.Duration
}

// InstallConfig contains installation options.
type InstallConfig struct {
	// Path where browsers are installed (PLAYWRIGHT_BROWSERS_PATH).
	Path string
	// Auto enables automatic installation on startup.
	Auto bool
	// WithDeps installs system dependencies (requires sudo on Linux).
	WithDeps bool
}

// PoolConfig contains page pool options.
type PoolConfig struct {
	// MinPages is the minimum number of pages to pre-warm.
	MinPages int
	// MaxPages is the maximum number of pages allowed.
	MaxPages int
	// TTL is the time-to-live for shared pages. After TTL the page is recycled.
	TTL time.Duration
	// GracePeriod is the window before TTL expiry during which a page stops
	// accepting new operations, and the window after TTL expiry to wait for
	// active operations to drain before force-closing.
	GracePeriod time.Duration
	// OperationTimeout is the timeout for a single Do/DoShare call including
	// retries.
	OperationTimeout time.Duration
	// InitTimeout is the timeout for page initialization.
	InitTimeout time.Duration
	// HealthCheckInterval is the interval for background health checks.
	HealthCheckInterval time.Duration
	// ScheduleStrategy is the page scheduling strategy.
	ScheduleStrategy string
}

// Config is the complete configuration.
type Config struct {
	Browser BrowserConfig
	Install InstallConfig
	Pool    PoolConfig
	Logger  Logger
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Browser: BrowserConfig{
			Headless: true,
			Args: []string{
				"--no-sandbox",
				"--disable-dev-shm-usage",
				"--disable-gpu",
				"--disable-software-rasterizer",
				"--disable-extensions",
				"--disable-background-networking",
				"--disable-background-timer-throttling",
				"--disable-backgrounding-occluded-windows",
				"--disable-renderer-backgrounding",
				"--disable-features=IsolateOrigins,site-per-process",
				"--disable-site-isolation-trials",
				"--disable-web-security",
				"--disable-features=VizDisplayCompositor",
				"--no-first-run",
				"--no-zygote",
				"--disable-setuid-sandbox",
				"--disable-breakpad",
				"--disable-component-update",
				"--disable-default-apps",
				"--disable-translate",
				"--metrics-recording-only",
				"--mute-audio",
				"--blink-settings=imagesEnabled=false",
			},
			Timeout: 60 * time.Second,
		},
		Install: InstallConfig{
			Path:     defaultInstallPath,
			Auto:     true,
			WithDeps: true,
		},
		Pool: PoolConfig{
			MinPages:            1,
			MaxPages:            10,
			TTL:                 30 * time.Minute,
			GracePeriod:         10 * time.Second,
			OperationTimeout:    30 * time.Second,
			InitTimeout:         30 * time.Second,
			HealthCheckInterval: 30 * time.Second,
			ScheduleStrategy:    "round-robin",
		},
	}
}

// Option is a function that modifies the Config.
type Option func(*Config)

// --- Browser Options ---

func WithHeadless(headless bool) Option {
	return func(c *Config) { c.Browser.Headless = headless }
}

func WithBrowserArgs(args ...string) Option {
	return func(c *Config) { c.Browser.Args = args }
}

func WithBrowserTimeout(timeout time.Duration) Option {
	return func(c *Config) { c.Browser.Timeout = timeout }
}

// --- Install Options ---

func WithInstallPath(path string) Option {
	return func(c *Config) { c.Install.Path = path }
}

func WithAutoInstall(auto bool) Option {
	return func(c *Config) { c.Install.Auto = auto }
}

func WithDeps(withDeps bool) Option {
	return func(c *Config) { c.Install.WithDeps = withDeps }
}

// --- Pool Options ---

func WithMinPages(min int) Option {
	return func(c *Config) { c.Pool.MinPages = min }
}

func WithMaxPages(max int) Option {
	return func(c *Config) { c.Pool.MaxPages = max }
}

func WithPoolTTL(ttl time.Duration) Option {
	return func(c *Config) { c.Pool.TTL = ttl }
}

func WithGracePeriod(d time.Duration) Option {
	return func(c *Config) { c.Pool.GracePeriod = d }
}

func WithOperationTimeout(timeout time.Duration) Option {
	return func(c *Config) { c.Pool.OperationTimeout = timeout }
}

func WithInitTimeout(timeout time.Duration) Option {
	return func(c *Config) { c.Pool.InitTimeout = timeout }
}

func WithHealthCheckInterval(interval time.Duration) Option {
	return func(c *Config) { c.Pool.HealthCheckInterval = interval }
}

func WithScheduleStrategy(strategy string) Option {
	return func(c *Config) { c.Pool.ScheduleStrategy = strategy }
}

// --- Logger Option ---

func WithLogger(l Logger) Option {
	return func(c *Config) { c.Logger = l }
}

// NewConfig creates a new Config with options applied.
func NewConfig(opts ...Option) *Config {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// --- Session Options (per-session pool overrides) ---

// SessionOption overrides pool configuration for a specific session.
type SessionOption func(*PoolConfig)

func WithSessionMinPages(min int) SessionOption {
	return func(p *PoolConfig) { p.MinPages = min }
}

func WithSessionMaxPages(max int) SessionOption {
	return func(p *PoolConfig) { p.MaxPages = max }
}

func WithSessionTTL(ttl time.Duration) SessionOption {
	return func(p *PoolConfig) { p.TTL = ttl }
}

func WithSessionGracePeriod(d time.Duration) SessionOption {
	return func(p *PoolConfig) { p.GracePeriod = d }
}

func WithSessionOperationTimeout(timeout time.Duration) SessionOption {
	return func(p *PoolConfig) { p.OperationTimeout = timeout }
}

func WithSessionInitTimeout(timeout time.Duration) SessionOption {
	return func(p *PoolConfig) { p.InitTimeout = timeout }
}

func WithSessionHealthCheckInterval(interval time.Duration) SessionOption {
	return func(p *PoolConfig) { p.HealthCheckInterval = interval }
}

func WithSessionScheduleStrategy(strategy string) SessionOption {
	return func(p *PoolConfig) { p.ScheduleStrategy = strategy }
}
