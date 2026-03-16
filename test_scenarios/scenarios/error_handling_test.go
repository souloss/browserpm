package scenarios

import (
	"errors"
	"testing"

	"github.com/souloss/browserpm"
	"github.com/souloss/browserpm/test_scenarios/helpers"
)

func TestErrorCodeMatching(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected browserpm.ErrorCode
	}{
		{
			name:     "session_not_found",
			err:      browserpm.NewError(browserpm.ErrSessionNotFound, "session not found"),
			expected: browserpm.ErrSessionNotFound,
		},
		{
			name:     "session_exists",
			err:      browserpm.NewError(browserpm.ErrSessionExists, "session exists"),
			expected: browserpm.ErrSessionExists,
		},
		{
			name:     "pool_exhausted",
			err:      browserpm.NewError(browserpm.ErrPoolExhausted, "pool exhausted"),
			expected: browserpm.ErrPoolExhausted,
		},
		{
			name:     "context_dead",
			err:      browserpm.NewError(browserpm.ErrContextDead, "context dead"),
			expected: browserpm.ErrContextDead,
		},
		{
			name:     "page_unavailable",
			err:      browserpm.NewError(browserpm.ErrPageUnavailable, "page unavailable"),
			expected: browserpm.ErrPageUnavailable,
		},
		{
			name:     "timeout",
			err:      browserpm.NewError(browserpm.ErrTimeout, "timeout"),
			expected: browserpm.ErrTimeout,
		},
		{
			name:     "closed",
			err:      browserpm.NewError(browserpm.ErrClosed, "closed"),
			expected: browserpm.ErrClosed,
		},
		{
			name:     "invalid_state",
			err:      browserpm.NewError(browserpm.ErrInvalidState, "invalid state"),
			expected: browserpm.ErrInvalidState,
		},
		{
			name:     "internal",
			err:      browserpm.NewError(browserpm.ErrInternal, "internal error"),
			expected: browserpm.ErrInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			helpers.AssertErrorCode(t, tc.err, tc.expected)
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	cause := errors.New("underlying error")
	wrapped := browserpm.WrapError(cause, browserpm.ErrContextDead, "context failed")

	helpers.AssertErrorCode(t, wrapped, browserpm.ErrContextDead)

	var bpmErr *browserpm.BpmError
	if !errors.As(wrapped, &bpmErr) {
		t.Fatal("expected error to be BpmError")
	}

	if bpmErr.Cause != cause {
		t.Errorf("expected cause to be preserved, got %v", bpmErr.Cause)
	}
}

func TestErrorIsMatching(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		match  bool
	}{
		{
			name:   "same_code_match",
			err:    browserpm.NewError(browserpm.ErrSessionNotFound, "not found"),
			target: browserpm.ErrSessionNotFoundErr,
			match:  true,
		},
		{
			name:   "different_code_no_match",
			err:    browserpm.NewError(browserpm.ErrSessionNotFound, "not found"),
			target: browserpm.ErrSessionExistsErr,
			match:  false,
		},
		{
			name:   "wrapped_error_match",
			err:    browserpm.WrapError(errors.New("cause"), browserpm.ErrContextDead, "wrapped"),
			target: browserpm.ErrContextDeadErr,
			match:  true,
		},
		{
			name:   "closed_error_match",
			err:    browserpm.NewError(browserpm.ErrClosed, "manager closed"),
			target: browserpm.ErrClosedErr,
			match:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isMatch := errors.Is(tc.err, tc.target)
			if isMatch != tc.match {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tc.err, tc.target, isMatch, tc.match)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	wrapped := browserpm.WrapError(cause, browserpm.ErrInternal, "wrapped error")

	unwrapped := errors.Unwrap(wrapped)
	if unwrapped != cause {
		t.Errorf("Unwrap: expected %v, got %v", cause, unwrapped)
	}
}

func TestErrorMessageFormat(t *testing.T) {
	err := browserpm.NewError(browserpm.ErrSessionNotFound, "test session")
	expected := "browserpm [SessionNotFound]: test session"

	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestWrappedErrorMessageFormat(t *testing.T) {
	cause := errors.New("underlying")
	err := browserpm.WrapError(cause, browserpm.ErrContextDead, "context failed")

	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() should not be empty")
	}

	var bpmErr *browserpm.BpmError
	if !errors.As(err, &bpmErr) {
		t.Fatal("expected BpmError")
	}

	if bpmErr.Code != browserpm.ErrContextDead {
		t.Errorf("expected code ErrContextDead, got %v", bpmErr.Code)
	}

	if bpmErr.Message != "context failed" {
		t.Errorf("expected message 'context failed', got %q", bpmErr.Message)
	}

	if bpmErr.Cause != cause {
		t.Errorf("expected cause %v, got %v", cause, bpmErr.Cause)
	}
}

func TestErrorCodeString(t *testing.T) {
	codes := []browserpm.ErrorCode{
		browserpm.ErrSessionNotFound,
		browserpm.ErrSessionExists,
		browserpm.ErrPoolExhausted,
		browserpm.ErrContextDead,
		browserpm.ErrPageUnavailable,
		browserpm.ErrTimeout,
		browserpm.ErrClosed,
		browserpm.ErrInvalidState,
		browserpm.ErrInternal,
	}

	for _, code := range codes {
		s := code.String()
		if s == "" {
			t.Errorf("ErrorCode(%d).String() should not be empty", code)
		}
		t.Logf("ErrorCode %d = %s", code, s)
	}
}

func TestBusinessVsSystemError(t *testing.T) {
	businessErr := errors.New("invalid selector")
	systemErr := browserpm.NewError(browserpm.ErrContextDead, "context died")

	var bpmErr *browserpm.BpmError
	if errors.As(businessErr, &bpmErr) {
		t.Error("business error should not be BpmError")
	}

	if !errors.As(systemErr, &bpmErr) {
		t.Error("system error should be BpmError")
	}
}

func TestErrorChaining(t *testing.T) {
	root := errors.New("root cause")
	l1 := browserpm.WrapError(root, browserpm.ErrInternal, "level 1")
	l2 := browserpm.WrapError(l1, browserpm.ErrContextDead, "level 2")

	var bpmErr *browserpm.BpmError
	if !errors.As(l2, &bpmErr) {
		t.Fatal("expected BpmError")
	}

	if bpmErr.Code != browserpm.ErrContextDead {
		t.Errorf("expected ErrContextDead, got %v", bpmErr.Code)
	}

	helpers.AssertErrorIs(t, l2, browserpm.ErrContextDeadErr)
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []struct {
		name string
		err  *browserpm.BpmError
		code browserpm.ErrorCode
	}{
		{"SessionNotFound", browserpm.ErrSessionNotFoundErr, browserpm.ErrSessionNotFound},
		{"SessionExists", browserpm.ErrSessionExistsErr, browserpm.ErrSessionExists},
		{"PoolExhausted", browserpm.ErrPoolExhaustedErr, browserpm.ErrPoolExhausted},
		{"ContextDead", browserpm.ErrContextDeadErr, browserpm.ErrContextDead},
		{"PageUnavailable", browserpm.ErrPageUnavailableErr, browserpm.ErrPageUnavailable},
		{"Timeout", browserpm.ErrTimeoutErr, browserpm.ErrTimeout},
		{"Closed", browserpm.ErrClosedErr, browserpm.ErrClosed},
		{"InvalidState", browserpm.ErrInvalidStateErr, browserpm.ErrInvalidState},
		{"Internal", browserpm.ErrInternalErr, browserpm.ErrInternal},
	}

	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.err.Code != s.code {
				t.Errorf("sentinel %s has wrong code: got %v, want %v", s.name, s.err.Code, s.code)
			}
		})
	}
}
