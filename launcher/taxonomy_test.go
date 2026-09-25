package main

import (
	"strings"
	"testing"
)

func TestStateStrings(t *testing.T) {
	want := map[ServerState]string{
		StateConnecting:   "Connecting",
		StateOnline:       "Online",
		StateOffline:      "Offline",
		StateTimeout:      "Timeout",
		StateUnauthorized: "Unauthorized",
		StateServerError:  "Server Error",
		StateMalformed:    "Malformed Response",
	}
	for st, label := range want {
		if st.String() != label {
			t.Errorf("%d.String() = %q, want %q", st, st.String(), label)
		}
	}
}

func TestSanitizeDetail(t *testing.T) {
	got := sanitizeDetail("hello\x00\nworld")
	if strings.ContainsAny(got, "\x00\n") {
		t.Errorf("control chars not stripped: %q", got)
	}
	long := sanitizeDetail(strings.Repeat("x", 500))
	if len(long) > 163 {
		t.Errorf("detail not bounded: %d chars", len(long))
	}
}

func TestControlOpValidity(t *testing.T) {
	if !OpRestart.Valid() || !OpStop.Valid() {
		t.Error("restart/stop must be valid")
	}
	for _, bad := range []ControlOp{"", "status", "rm", "restart ", "Restart"} {
		if bad.Valid() {
			t.Errorf("%q must be invalid", bad)
		}
	}
}
