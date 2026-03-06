package browserpm

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() should not return nil")
	}
	if !cfg.Browser.Headless {
		t.Error("Browser.Headless should default to true")
	}
	if cfg.Browser.Timeout != 60*time.Second {
		t.Errorf("Browser.Timeout = %v, want 60s", cfg.Browser.Timeout)
	}
	if len(cfg.Browser.Args) == 0 {
		t.Error("Browser.Args should not be empty")
	}
	if !cfg.Install.Auto {
		t.Error("Install.Auto should default to true")
	}
	if cfg.Pool.MinPages != 1 {
		t.Errorf("Pool.MinPages = %d, want 1", cfg.Pool.MinPages)
	}
	if cfg.Pool.MaxPages != 10 {
		t.Errorf("Pool.MaxPages = %d, want 10", cfg.Pool.MaxPages)
	}
	if cfg.Pool.TTL != 30*time.Minute {
		t.Errorf("Pool.TTL = %v, want 30m", cfg.Pool.TTL)
	}
	if cfg.Pool.GracePeriod != 10*time.Second {
		t.Errorf("Pool.GracePeriod = %v, want 10s", cfg.Pool.GracePeriod)
	}
	if cfg.Pool.ScheduleStrategy != "round-robin" {
		t.Errorf("Pool.ScheduleStrategy = %q, want round-robin", cfg.Pool.ScheduleStrategy)
	}
}

func TestNewConfig_DefaultsAreValid(t *testing.T) {
	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}
	if cfg.Pool.MinPages != 1 {
		t.Errorf("expected MinPages=1, got %d", cfg.Pool.MinPages)
	}
	if cfg.Pool.MaxPages != 10 {
		t.Errorf("expected MaxPages=10, got %d", cfg.Pool.MaxPages)
	}
}

func TestNewConfig_WithOptions(t *testing.T) {
	cfg, err := NewConfig(
		WithHeadless(false),
		WithMinPages(5),
		WithMaxPages(20),
		WithPoolTTL(10*time.Minute),
		WithGracePeriod(5*time.Second),
		WithOperationTimeout(60*time.Second),
		WithScheduleStrategy("least-busy"),
	)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.Browser.Headless {
		t.Error("WithHeadless(false) should set Headless to false")
	}
	if cfg.Pool.MinPages != 5 {
		t.Errorf("MinPages = %d, want 5", cfg.Pool.MinPages)
	}
	if cfg.Pool.MaxPages != 20 {
		t.Errorf("MaxPages = %d, want 20", cfg.Pool.MaxPages)
	}
	if cfg.Pool.TTL != 10*time.Minute {
		t.Errorf("TTL = %v, want 10m", cfg.Pool.TTL)
	}
	if cfg.Pool.GracePeriod != 5*time.Second {
		t.Errorf("GracePeriod = %v, want 5s", cfg.Pool.GracePeriod)
	}
	if cfg.Pool.OperationTimeout != 60*time.Second {
		t.Errorf("OperationTimeout = %v, want 60s", cfg.Pool.OperationTimeout)
	}
	if cfg.Pool.ScheduleStrategy != "least-busy" {
		t.Errorf("ScheduleStrategy = %q, want least-busy", cfg.Pool.ScheduleStrategy)
	}
}

func TestNewConfig_WithBrowserArgs(t *testing.T) {
	args := []string{"--custom-arg", "--another"}
	cfg, err := NewConfig(WithBrowserArgs(args...))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if len(cfg.Browser.Args) != 2 {
		t.Fatalf("Args length = %d, want 2", len(cfg.Browser.Args))
	}
	if cfg.Browser.Args[0] != "--custom-arg" || cfg.Browser.Args[1] != "--another" {
		t.Errorf("Args = %v", cfg.Browser.Args)
	}
}

func TestNewConfig_WithLogger(t *testing.T) {
	log := NewNopLogger()
	cfg, err := NewConfig(WithLogger(log))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.Logger != log {
		t.Error("WithLogger should set Logger")
	}
}

func TestNewConfig_MinPagesExceedsMaxPages(t *testing.T) {
	_, err := NewConfig(WithMinPages(20), WithMaxPages(5))
	if err == nil {
		t.Fatal("expected error when MinPages > MaxPages")
	}
	if !strings.Contains(err.Error(), "MinPages") {
		t.Errorf("error should mention MinPages, got: %v", err)
	}
}

func TestNewConfig_NegativeMinPages(t *testing.T) {
	_, err := NewConfig(WithMinPages(-1))
	if err == nil {
		t.Fatal("expected error for negative MinPages")
	}
}

func TestNewConfig_ZeroMaxPages(t *testing.T) {
	_, err := NewConfig(WithMaxPages(0))
	if err == nil {
		t.Fatal("expected error for MaxPages=0")
	}
}

func TestNewConfig_NegativeTTL(t *testing.T) {
	_, err := NewConfig(WithPoolTTL(-1 * time.Second))
	if err == nil {
		t.Fatal("expected error for negative TTL")
	}
}

func TestNewConfig_GracePeriodExceedsTTL(t *testing.T) {
	_, err := NewConfig(
		WithPoolTTL(10*time.Second),
		WithGracePeriod(15*time.Second),
	)
	if err == nil {
		t.Fatal("expected error when GracePeriod >= TTL")
	}
}

func TestNewConfig_ZeroOperationTimeout(t *testing.T) {
	_, err := NewConfig(WithOperationTimeout(0))
	if err == nil {
		t.Fatal("expected error for zero OperationTimeout")
	}
}

func TestNewConfig_ZeroInitTimeout(t *testing.T) {
	_, err := NewConfig(WithInitTimeout(0))
	if err == nil {
		t.Fatal("expected error for zero InitTimeout")
	}
}

func TestNewConfig_NegativeHealthCheckInterval(t *testing.T) {
	_, err := NewConfig(WithHealthCheckInterval(-1 * time.Second))
	if err == nil {
		t.Fatal("expected error for negative HealthCheckInterval")
	}
}

func TestNewConfig_ZeroBrowserTimeout(t *testing.T) {
	_, err := NewConfig(WithBrowserTimeout(0))
	if err == nil {
		t.Fatal("expected error for zero Browser.Timeout")
	}
}

func TestNewConfig_DisabledTTLAllowsAnyGracePeriod(t *testing.T) {
	// TTL=0 means disabled, so GracePeriod should be irrelevant
	_, err := NewConfig(WithPoolTTL(0), WithGracePeriod(10*time.Second))
	if err != nil {
		t.Fatalf("TTL=0 should skip GracePeriod check, got: %v", err)
	}
}

func TestNewConfig_DisabledHealthCheck(t *testing.T) {
	// HealthCheckInterval=0 means disabled
	_, err := NewConfig(WithHealthCheckInterval(0))
	if err != nil {
		t.Fatalf("HealthCheckInterval=0 should be valid (disabled), got: %v", err)
	}
}

func TestSessionOptions_ApplyToPoolConfig(t *testing.T) {
	base := DefaultConfig().Pool
	opts := []SessionOption{
		WithSessionMinPages(3),
		WithSessionMaxPages(15),
		WithSessionTTL(15 * time.Minute),
		WithSessionGracePeriod(20 * time.Second),
		WithSessionOperationTimeout(45 * time.Second),
		WithSessionInitTimeout(60 * time.Second),
		WithSessionHealthCheckInterval(1 * time.Minute),
		WithSessionScheduleStrategy("random"),
	}
	for _, opt := range opts {
		opt(&base)
	}
	if base.MinPages != 3 {
		t.Errorf("MinPages = %d, want 3", base.MinPages)
	}
	if base.MaxPages != 15 {
		t.Errorf("MaxPages = %d, want 15", base.MaxPages)
	}
	if base.TTL != 15*time.Minute {
		t.Errorf("TTL = %v", base.TTL)
	}
	if base.GracePeriod != 20*time.Second {
		t.Errorf("GracePeriod = %v", base.GracePeriod)
	}
	if base.OperationTimeout != 45*time.Second {
		t.Errorf("OperationTimeout = %v", base.OperationTimeout)
	}
	if base.InitTimeout != 60*time.Second {
		t.Errorf("InitTimeout = %v", base.InitTimeout)
	}
	if base.HealthCheckInterval != 1*time.Minute {
		t.Errorf("HealthCheckInterval = %v", base.HealthCheckInterval)
	}
	if base.ScheduleStrategy != "random" {
		t.Errorf("ScheduleStrategy = %q", base.ScheduleStrategy)
	}
}

func TestPoolConfig_Validate(t *testing.T) {
	valid := PoolConfig{
		MinPages:            2,
		MaxPages:            5,
		TTL:                 10 * time.Minute,
		GracePeriod:         30 * time.Second,
		OperationTimeout:    15 * time.Second,
		InitTimeout:         15 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		ScheduleStrategy:    "round-robin",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}
