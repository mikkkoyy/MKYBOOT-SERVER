package main

import (
	"strings"
	"testing"
)

func TestStateStrings(t *testing.T) {
	want := map[ServerState]string{
		StateConnecting:   "Connecting",
		StateStarting:     "Starting",
		StateOnline:       "Online",
		StateDegraded:     "Degraded",
		StateOffline:      "Offline",
		StateTimeout:      "Slow Response",
		StateUnauthorized: "Unauthorized",
		StateServerError:  "Server Error",
		StateMalformed:    "Malformed Response",
		StateStopping:     "Stopping",
	}
	for st, label := range want {
		if st.String() != label {
			t.Errorf("%d.String() = %q, want %q", st, st.String(), label)
		}
	}
}

// A reachable server must never be reported as unreachable, and vice versa.
// The control actions key off canControlServer, so an incorrect Reachable()
// verdict is what strands the user with every action disabled.
func TestStateReachability(t *testing.T) {
	reachable := []ServerState{
		StateOnline, StateDegraded, StateUnauthorized, StateStarting, StateStopping,
	}
	for _, st := range reachable {
		if !st.Reachable() {
			t.Errorf("%s must count as reachable", st)
		}
	}
	unreachable := []ServerState{
		StateConnecting, StateOffline, StateTimeout, StateServerError, StateMalformed,
	}
	for _, st := range unreachable {
		if st.Reachable() {
			t.Errorf("%s must not count as reachable", st)
		}
	}
}

// canControlServer is what enables Restart/Stop. nginx serves the control
// endpoint, so any state where nginx answers must allow control; Degraded
// included, because nginx is up and only the diskless services are down.
func TestCanControlServer(t *testing.T) {
	allowed := []ServerState{StateOnline, StateDegraded, StateUnauthorized}
	for _, st := range allowed {
		if !canControlServer(st) {
			t.Errorf("%s should allow control", st)
		}
	}
	denied := []ServerState{
		StateConnecting, StateStarting, StateStopping, StateOffline,
		StateTimeout, StateServerError, StateMalformed,
	}
	for _, st := range denied {
		if canControlServer(st) {
			t.Errorf("%s should not allow control", st)
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
