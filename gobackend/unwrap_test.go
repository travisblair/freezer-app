package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder is an inner ResponseWriter that implements
// SetWriteDeadline, so we can prove the deadline call reaches it
// through the responseWriter wrapper's Unwrap() chain.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlineSet time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadlineSet = t
	return nil
}

// TestResponseWriterUnwrap verifies the logging middleware's wrapper
// exposes Unwrap() so http.ResponseController can reach the underlying
// writer. The tarpit depends on this: SetWriteDeadline(time.Time{}) must
// succeed through the full chain, or the global 30s WriteTimeout kills
// tarpit responses at 30s instead of their intended 10-minute run.
func TestResponseWriterUnwrap(t *testing.T) {
	inner := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	rw := &responseWriter{ResponseWriter: inner, statusCode: 200}

	rc := http.NewResponseController(rw)
	want := time.Now().Add(10 * time.Minute)
	if err := rc.SetWriteDeadline(want); err != nil {
		t.Fatalf("SetWriteDeadline through responseWriter = %v; Unwrap() missing or broken — tarpit would die at the global 30s WriteTimeout", err)
	}
	if !inner.deadlineSet.Equal(want) {
		t.Fatalf("deadline never reached the underlying writer (got %v, want %v) — Unwrap() chain is broken", inner.deadlineSet, want)
	}
}
