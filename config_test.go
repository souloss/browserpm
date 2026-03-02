package browserpm

import (
	"strings"
	"testing"
	"time"
)

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
