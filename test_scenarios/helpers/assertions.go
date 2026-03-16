package helpers

import (
	"errors"
	"testing"
	"time"

	"github.com/souloss/browserpm"
)

func AssertErrorCode(t *testing.T, err error, expected browserpm.ErrorCode) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error with code %s, got nil", expected)
	}

	var bpmErr *browserpm.BpmError
	if !errors.As(err, &bpmErr) {
		t.Fatalf("expected BpmError, got %T: %v", err, err)
	}

	if bpmErr.Code != expected {
		t.Errorf("expected error code %s, got %s (message: %s)", expected, bpmErr.Code, bpmErr.Message)
	}
}

func AssertErrorIs(t *testing.T, err error, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Errorf("expected error to be %v, got %v", target, err)
	}
}

func AssertNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func AssertSessionActive(t *testing.T, session *browserpm.Session) {
	t.Helper()

	info := session.Status()
	if info.State != browserpm.SessionActive {
		t.Errorf("expected session state to be active, got %s", info.State)
	}
}

func AssertSessionClosed(t *testing.T, session *browserpm.Session) {
	t.Helper()

	info := session.Status()
	if info.State != browserpm.SessionClosed {
		t.Errorf("expected session state to be closed, got %s", info.State)
	}
}

func AssertSessionDegraded(t *testing.T, session *browserpm.Session) {
	t.Helper()

	info := session.Status()
	if info.State != browserpm.SessionDegraded {
		t.Errorf("expected session state to be degraded, got %s", info.State)
	}
}

func AssertSessionHealthy(t *testing.T, session *browserpm.Session) {
	t.Helper()

	if !session.IsHealthy() {
		t.Error("expected session to be healthy")
	}
}

func AssertSessionNotHealthy(t *testing.T, session *browserpm.Session) {
	t.Helper()

	if session.IsHealthy() {
		t.Error("expected session to not be healthy")
	}
}

func AssertPageCount(t *testing.T, session *browserpm.Session, expected int) {
	t.Helper()

	info := session.Status()
	if info.PageCount != expected {
		t.Errorf("expected page count %d, got %d", expected, info.PageCount)
	}
}

func AssertPageCountInRange(t *testing.T, session *browserpm.Session, min, max int) {
	t.Helper()

	info := session.Status()
	if info.PageCount < min || info.PageCount > max {
		t.Errorf("expected page count in range [%d, %d], got %d", min, max, info.PageCount)
	}
}

func AssertActiveOps(t *testing.T, session *browserpm.Session, expected int64) {
	t.Helper()

	info := session.Status()
	if info.ActiveOps != expected {
		t.Errorf("expected active ops %d, got %d", expected, info.ActiveOps)
	}
}

func AssertSessionExists(t *testing.T, manager *browserpm.BrowserManager, name string) {
	t.Helper()

	sessions := manager.ListSessions()
	for _, s := range sessions {
		if s.Name == name {
			return
		}
	}
	t.Errorf("expected session %q to exist", name)
}

func AssertSessionNotExists(t *testing.T, manager *browserpm.BrowserManager, name string) {
	t.Helper()

	sessions := manager.ListSessions()
	for _, s := range sessions {
		if s.Name == name {
			t.Errorf("expected session %q to not exist", name)
			return
		}
	}
}

func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration, interval time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	t.Error("condition did not become true within timeout")
}

func AssertNever(t *testing.T, condition func() bool, duration time.Duration, interval time.Duration) {
	t.Helper()

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if condition() {
			t.Error("condition became true when it should not have")
			return
		}
		time.Sleep(interval)
	}
}

func AssertPoolStats(t *testing.T, session *browserpm.Session, check func(stats *browserpm.PoolStats) bool) {
	t.Helper()

	stats := session.Stats()
	if stats == nil {
		t.Fatal("expected pool stats, got nil")
	}

	if !check(stats) {
		t.Errorf("pool stats check failed: %+v", stats)
	}
}

type ErrorMatcher struct {
	t   *testing.T
	err error
}

func ExpectError(t *testing.T, err error) *ErrorMatcher {
	t.Helper()
	return &ErrorMatcher{t: t, err: err}
}

func (m *ErrorMatcher) ToHaveCode(code browserpm.ErrorCode) *ErrorMatcher {
	m.t.Helper()
	AssertErrorCode(m.t, m.err, code)
	return m
}

func (m *ErrorMatcher) ToBe(target error) *ErrorMatcher {
	m.t.Helper()
	AssertErrorIs(m.t, m.err, target)
	return m
}

func (m *ErrorMatcher) ToNotExist() *ErrorMatcher {
	m.t.Helper()
	AssertNoError(m.t, m.err)
	return m
}

func (m *ErrorMatcher) ToContainMessage(substr string) *ErrorMatcher {
	m.t.Helper()

	if m.err == nil {
		m.t.Fatalf("expected error to contain %q, got nil", substr)
	}

	var bpmErr *browserpm.BpmError
	if errors.As(m.err, &bpmErr) {
		if !contains(bpmErr.Message, substr) {
			m.t.Errorf("expected error message to contain %q, got %q", substr, bpmErr.Message)
		}
	} else {
		if !contains(m.err.Error(), substr) {
			m.t.Errorf("expected error to contain %q, got %q", substr, m.err.Error())
		}
	}
	return m
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
