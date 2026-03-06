package browserpm

import (
	"errors"
	"strings"
	"testing"
)

func TestBpmError_Error(t *testing.T) {
	e := NewError(ErrSessionNotFound, "session x not found")
	got := e.Error()
	if got == "" {
		t.Fatal("Error() should not return empty string")
	}
	if !strings.Contains(got, "browserpm") {
		t.Errorf("Error() should contain 'browserpm', got %q", got)
	}
}

func TestBpmError_ErrorWithCause(t *testing.T) {
	cause := errors.New("underlying")
	e := WrapError(cause, ErrSessionNotFound, "wrapped")
	got := e.Error()
	if got == "" {
		t.Fatal("Error() should not return empty string")
	}
	if !errors.Is(e, cause) {
		t.Error("WrapError should be unwrappable to cause")
	}
}

func TestBpmError_Unwrap(t *testing.T) {
	cause := errors.New("cause")
	e := WrapError(cause, ErrInternal, "msg")
	if e.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", e.Unwrap(), cause)
	}

	e2 := NewError(ErrSessionNotFound, "msg")
	if e2.Unwrap() != nil {
		t.Errorf("NewError Unwrap() should be nil, got %v", e2.Unwrap())
	}
}

func TestBpmError_Is(t *testing.T) {
	e := NewError(ErrSessionNotFound, "msg")
	if !errors.Is(e, ErrSessionNotFoundErr) {
		t.Error("errors.Is(e, ErrSessionNotFoundErr) should be true")
	}
	if errors.Is(e, ErrSessionExistsErr) {
		t.Error("errors.Is(e, ErrSessionExistsErr) should be false")
	}

	wrapped := WrapError(e, ErrInternal, "wrapped")
	if !errors.Is(wrapped, ErrInternalErr) {
		t.Error("errors.Is(wrapped, ErrInternalErr) should be true")
	}
	// wrapped wraps e (ErrSessionNotFound), so chain contains both
	if !errors.Is(wrapped, ErrSessionNotFoundErr) {
		t.Error("errors.Is(wrapped, ErrSessionNotFoundErr) should be true (cause in chain)")
	}
}

func TestBpmError_As(t *testing.T) {
	e := NewError(ErrPoolExhausted, "pool full")
	var target *BpmError
	if !errors.As(e, &target) {
		t.Fatal("errors.As should succeed")
	}
	if target.Code != ErrPoolExhausted {
		t.Errorf("target.Code = %v, want ErrPoolExhausted", target.Code)
	}
	if target.Message != "pool full" {
		t.Errorf("target.Message = %q, want %q", target.Message, "pool full")
	}
}

func TestErrorCode_String(t *testing.T) {
	if ErrSessionNotFound.String() != "SessionNotFound" {
		t.Errorf("ErrSessionNotFound.String() = %q", ErrSessionNotFound.String())
	}
	if ErrInternal.String() != "Internal" {
		t.Errorf("ErrInternal.String() = %q", ErrInternal.String())
	}
	if ErrorCode(999).String() == "" {
		t.Error("unknown ErrorCode should have non-empty String()")
	}
}

func TestNewError(t *testing.T) {
	e := NewError(ErrClosed, "manager closed")
	if e.Code != ErrClosed {
		t.Errorf("Code = %v", e.Code)
	}
	if e.Message != "manager closed" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Cause != nil {
		t.Error("Cause should be nil")
	}
}

func TestWrapError(t *testing.T) {
	cause := errors.New("io error")
	e := WrapError(cause, ErrPageUnavailable, "page init failed")
	if e.Code != ErrPageUnavailable {
		t.Errorf("Code = %v", e.Code)
	}
	if e.Cause != cause {
		t.Errorf("Cause = %v", e.Cause)
	}
}
