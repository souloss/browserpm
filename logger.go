// Package browserpm provides a logging interface and Zap implementation for the browser manager.
package browserpm

import "go.uber.org/zap"

// Level represents log level
type Level int

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in production.
	DebugLevel Level = iota
	// InfoLevel is the default logging priority.
	InfoLevel
	// ErrorLevel logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	ErrorLevel
)

// Field represents a structured log field
type Field struct {
	Key   string
	Value interface{}
}

// String creates a string field
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates an int field
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Int64 creates an int64 field
func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

// Duration creates a duration field
func Duration(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// Err creates an error field
func Err(err error) Field {
	return Field{Key: "error", Value: err}
}

// Any creates a field with any value
func Any(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// Logger is the interface for logging operations
type Logger interface {
	// Debug logs a message at DebugLevel
	Debug(msg string, fields ...Field)
	// Info logs a message at InfoLevel
	Info(msg string, fields ...Field)
	// Warn logs a message at WarnLevel
	Warn(msg string, fields ...Field)
	// Error logs a message at ErrorLevel with optional error
	Error(msg string, err error, fields ...Field)
	// With returns a logger with additional context fields
	With(fields ...Field) Logger
	// SetLevel sets the logging level
	SetLevel(level Level)
	// Sync flushes any buffered log entries
	Sync() error
}

// ZapAdapter implements Logger interface using zap
type ZapAdapter struct {
	logger *zap.Logger
	level  Level
}

// NewZapLogger creates a new ZapAdapter with default production config
func NewZapLogger() *ZapAdapter {
	zapLogger, _ := zap.NewProduction()
	return &ZapAdapter{
		logger: zapLogger,
		level:  InfoLevel,
	}
}

// NewZapLoggerWithConfig creates a new ZapAdapter with custom config
func NewZapLoggerWithConfig(debug bool) *ZapAdapter {
	var zapLogger *zap.Logger
	var err error
	if debug {
		config := zap.NewDevelopmentConfig()
		zapLogger, err = config.Build()
	} else {
		zapLogger, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
	return &ZapAdapter{
		logger: zapLogger,
		level:  InfoLevel,
	}
}

// NewZapLoggerFromZap creates a ZapAdapter from existing zap.Logger
func NewZapLoggerFromZap(zapLogger *zap.Logger) *ZapAdapter {
	return &ZapAdapter{
		logger: zapLogger,
		level:  InfoLevel,
	}
}

// convertFields converts Field slice to zap.Field slice
func convertFields(fields ...Field) []zap.Field {
	zapFields := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		switch v := f.Value.(type) {
		case string:
			zapFields = append(zapFields, zap.String(f.Key, v))
		case int:
			zapFields = append(zapFields, zap.Int(f.Key, v))
		case int64:
			zapFields = append(zapFields, zap.Int64(f.Key, v))
		case error:
			zapFields = append(zapFields, zap.Error(v))
		default:
			zapFields = append(zapFields, zap.Any(f.Key, v))
		}
	}
	return zapFields
}

// Debug logs a message at DebugLevel
func (l *ZapAdapter) Debug(msg string, fields ...Field) {
	if l.level <= DebugLevel {
		l.logger.Debug(msg, convertFields(fields...)...)
	}
}

// Info logs a message at InfoLevel
func (l *ZapAdapter) Info(msg string, fields ...Field) {
	if l.level <= InfoLevel {
		l.logger.Info(msg, convertFields(fields...)...)
	}
}

// Warn logs a message at WarnLevel
func (l *ZapAdapter) Warn(msg string, fields ...Field) {
	l.logger.Warn(msg, convertFields(fields...)...)
}

// Error logs a message at ErrorLevel
func (l *ZapAdapter) Error(msg string, err error, fields ...Field) {
	allFields := convertFields(fields...)
	if err != nil {
		allFields = append(allFields, zap.Error(err))
	}
	l.logger.Error(msg, allFields...)
}

// With returns a logger with additional context fields
func (l *ZapAdapter) With(fields ...Field) Logger {
	zapFields := convertFields(fields...)
	return &ZapAdapter{
		logger: l.logger.With(zapFields...),
		level:  l.level,
	}
}

// SetLevel sets the logging level
func (l *ZapAdapter) SetLevel(level Level) {
	l.level = level
}

// Sync flushes any buffered log entries
func (l *ZapAdapter) Sync() error {
	return l.logger.Sync()
}

// NopLogger is a no-op logger that discards all log messages
type NopLogger struct{}

// NewNopLogger creates a new no-op logger
func NewNopLogger() *NopLogger {
	return &NopLogger{}
}

// Debug discards the log message
func (l *NopLogger) Debug(msg string, fields ...Field) {}

// Info discards the log message
func (l *NopLogger) Info(msg string, fields ...Field) {}

// Warn discards the log message
func (l *NopLogger) Warn(msg string, fields ...Field) {}

// Error discards the log message
func (l *NopLogger) Error(msg string, err error, fields ...Field) {}

// With returns the same no-op logger
func (l *NopLogger) With(fields ...Field) Logger {
	return l
}

// SetLevel does nothing
func (l *NopLogger) SetLevel(level Level) {}

// Sync does nothing
func (l *NopLogger) Sync() error { return nil }
