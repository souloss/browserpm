package browserpm

import "fmt"

// ErrorCode represents the category of a browserpm error.
type ErrorCode int

const (
	ErrSessionNotFound ErrorCode = iota
	ErrSessionExists
	ErrPoolExhausted
	ErrContextDead
	ErrPageUnavailable
	ErrTimeout
	ErrClosed
	ErrInvalidState
	ErrInternal
)

var errorCodeNames = map[ErrorCode]string{
	ErrSessionNotFound: "SessionNotFound",
	ErrSessionExists:   "SessionExists",
	ErrPoolExhausted:   "PoolExhausted",
	ErrContextDead:     "ContextDead",
	ErrPageUnavailable: "PageUnavailable",
	ErrTimeout:         "Timeout",
	ErrClosed:          "Closed",
	ErrInvalidState:    "InvalidState",
	ErrInternal:        "Internal",
}

func (c ErrorCode) String() string {
	if name, ok := errorCodeNames[c]; ok {
		return name
	}
	return fmt.Sprintf("ErrorCode(%d)", int(c))
}

// BpmError is the standard error type for browserpm.
type BpmError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *BpmError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("browserpm [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("browserpm [%s]: %s", e.Code, e.Message)
}

func (e *BpmError) Unwrap() error {
	return e.Cause
}

// Is supports errors.Is matching by ErrorCode.
func (e *BpmError) Is(target error) bool {
	if t, ok := target.(*BpmError); ok {
		return e.Code == t.Code
	}
	return false
}

// NewError creates a BpmError without a cause.
func NewError(code ErrorCode, msg string) *BpmError {
	return &BpmError{Code: code, Message: msg}
}

// WrapError creates a BpmError wrapping an underlying cause.
func WrapError(err error, code ErrorCode, msg string) *BpmError {
	return &BpmError{Code: code, Message: msg, Cause: err}
}

// Sentinel errors for use with errors.Is.
var (
	ErrSessionNotFoundErr = &BpmError{Code: ErrSessionNotFound}
	ErrSessionExistsErr   = &BpmError{Code: ErrSessionExists}
	ErrPoolExhaustedErr   = &BpmError{Code: ErrPoolExhausted}
	ErrContextDeadErr     = &BpmError{Code: ErrContextDead}
	ErrPageUnavailableErr = &BpmError{Code: ErrPageUnavailable}
	ErrTimeoutErr         = &BpmError{Code: ErrTimeout}
	ErrClosedErr          = &BpmError{Code: ErrClosed}
	ErrInvalidStateErr    = &BpmError{Code: ErrInvalidState}
	ErrInternalErr        = &BpmError{Code: ErrInternal}
)
