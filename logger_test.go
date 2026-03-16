package browserpm

import (
	"errors"
	"testing"
)

func TestFieldConstructors(t *testing.T) {
	tests := []struct {
		name     string
		field    Field
		wantKey  string
		wantType string
	}{
		{
			name:     "String field",
			field:    String("key1", "value1"),
			wantKey:  "key1",
			wantType: "string",
		},
		{
			name:     "Int field",
			field:    Int("key2", 42),
			wantKey:  "key2",
			wantType: "int",
		},
		{
			name:     "Int64 field",
			field:    Int64("key3", int64(123)),
			wantKey:  "key3",
			wantType: "int64",
		},
		{
			name:     "Duration field",
			field:    Duration("key4", "1s"),
			wantKey:  "key4",
			wantType: "string",
		},
		{
			name:     "Err field",
			field:    Err(errors.New("test error")),
			wantKey:  "error",
			wantType: "*errors.errorString",
		},
		{
			name:     "Any field",
			field:    Any("key5", map[string]int{"a": 1}),
			wantKey:  "key5",
			wantType: "map[string]int",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.field.Key != tc.wantKey {
				t.Errorf("expected key %s, got %s", tc.wantKey, tc.field.Key)
			}
			if tc.field.Value == nil {
				t.Error("expected non-nil value")
			}
		})
	}
}

func TestLevel_Constants(t *testing.T) {
	if DebugLevel >= InfoLevel {
		t.Error("DebugLevel should be less than InfoLevel")
	}
	if InfoLevel >= ErrorLevel {
		t.Error("InfoLevel should be less than ErrorLevel")
	}
}

func TestNewNopLogger(t *testing.T) {
	logger := NewNopLogger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	logger.Debug("test debug", String("key", "value"))
	logger.Info("test info", Int("count", 1))
	logger.Warn("test warn", Err(errors.New("warning")))
	logger.Error("test error", errors.New("error"), String("detail", "test"))

	withLogger := logger.With(String("context", "test"))
	if withLogger == nil {
		t.Error("expected non-nil logger from With")
	}

	withLogger.SetLevel(DebugLevel)

	err := withLogger.Sync()
	if err != nil {
		t.Errorf("expected nil error from Sync, got %v", err)
	}
}

func TestNopLogger_Interface(t *testing.T) {
	var _ Logger = NewNopLogger()
}

func TestNewZapLogger(t *testing.T) {
	logger := NewZapLogger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	if logger.level != InfoLevel {
		t.Errorf("expected default level InfoLevel, got %d", logger.level)
	}
}

func TestNewZapLoggerWithConfig(t *testing.T) {
	t.Run("debug mode", func(t *testing.T) {
		logger := NewZapLoggerWithConfig(true)
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
	})

	t.Run("production mode", func(t *testing.T) {
		logger := NewZapLoggerWithConfig(false)
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
	})
}

func TestZapAdapter_LevelFiltering(t *testing.T) {
	logger := NewZapLogger()

	logger.SetLevel(ErrorLevel)
	if logger.level != ErrorLevel {
		t.Errorf("expected level ErrorLevel, got %d", logger.level)
	}

	logger.SetLevel(DebugLevel)
	if logger.level != DebugLevel {
		t.Errorf("expected level DebugLevel, got %d", logger.level)
	}
}

func TestZapAdapter_Logging(t *testing.T) {
	logger := NewZapLogger()

	logger.Debug("debug message", String("key", "value"))
	logger.Info("info message", Int("count", 1))
	logger.Warn("warn message")
	logger.Error("error message", errors.New("test error"))

	err := logger.Sync()
	if err != nil {
		t.Logf("Sync returned: %v (may be expected in test env)", err)
	}
}

func TestZapAdapter_With(t *testing.T) {
	logger := NewZapLogger()

	withLogger := logger.With(String("service", "browserpm"), Int("version", 1))
	if withLogger == nil {
		t.Fatal("expected non-nil logger from With")
	}

	withLogger.Info("test with context")
}

func TestZapAdapter_Interface(t *testing.T) {
	var _ Logger = NewZapLogger()
}

func TestConvertFields(t *testing.T) {
	fields := []Field{
		String("s", "value"),
		Int("i", 42),
		Int64("i64", int64(100)),
		Err(errors.New("test")),
		Any("map", map[string]int{"a": 1}),
	}

	zapFields := convertFields(fields...)
	if len(zapFields) != len(fields) {
		t.Errorf("expected %d zap fields, got %d", len(fields), len(zapFields))
	}
}

func TestZapLoggerFromZap(t *testing.T) {
	adapter := NewZapLoggerFromZap(nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter even with nil logger")
	}
}
