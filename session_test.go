package browserpm

import (
	"errors"
	"testing"
)

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "target closed error",
			err:      errors.New("target closed: could not read protocol padding: EOF"),
			expected: true,
		},
		{
			name:     "Target closed (capitalized)",
			err:      errors.New("Target closed: some error"),
			expected: true,
		},
		{
			name:     "Session closed error",
			err:      errors.New("Session closed unexpectedly"),
			expected: true,
		},
		{
			name:     "Connection closed error",
			err:      errors.New("Connection closed: remote hung up"),
			expected: true,
		},
		{
			name:     "Execution context was destroyed",
			err:      errors.New("Execution context was destroyed"),
			expected: true,
		},
		{
			name:     "execution context was destroyed (lowercase)",
			err:      errors.New("execution context was destroyed"),
			expected: true,
		},
		{
			name:     "could not read protocol",
			err:      errors.New("could not read protocol padding: EOF"),
			expected: true,
		},
		{
			name:     "protocol padding error",
			err:      errors.New("protocol padding error"),
			expected: true,
		},
		{
			name:     "frame was detached",
			err:      errors.New("frame was detached"),
			expected: true,
		},
		{
			name:     "page has been closed",
			err:      errors.New("page has been closed"),
			expected: true,
		},
		{
			name:     "websocket closed",
			err:      errors.New("websocket closed"),
			expected: true,
		},
		{
			name:     "socket hang up",
			err:      errors.New("socket hang up"),
			expected: true,
		},
		{
			name:     "business error - not a connection error",
			err:      errors.New("invalid argument"),
			expected: false,
		},
		{
			name:     "business error - timeout (should not match context dead)",
			err:      errors.New("context deadline exceeded"),
			expected: false,
		},
		{
			name:     "wrapped connection error",
			err:      errors.New("operation failed: target closed: EOF"),
			expected: true,
		},
		{
			name:     "complex error message with target closed",
			err:      errors.New("API 不可用: target closed: could not read protocol padding: EOF"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionError(tt.err)
			if got != tt.expected {
				t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestConnectionErrorPatterns(t *testing.T) {
	for _, pattern := range ConnectionErrorPatterns {
		if pattern == "" {
			t.Errorf("ConnectionErrorPatterns contains empty pattern")
		}
	}

	commonErrors := []string{
		"target closed",
		"Execution context was destroyed",
		"could not read protocol padding",
		"frame was detached",
		"page has been closed",
	}

	for _, errMsg := range commonErrors {
		if !IsConnectionError(errors.New(errMsg)) {
			t.Errorf("Common Playwright error %q is not detected by IsConnectionError", errMsg)
		}
	}
}
