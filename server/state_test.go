package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestState(t *testing.T) *state {
	t.Helper()
	s, err := newState(filepath.Join(t.TempDir(), "state.json"), 90*time.Second)
	if err != nil {
		t.Fatalf("newState: %v", err)
	}
	return s
}

func TestNormalizeMAC(t *testing.T) {
	valid := map[string]string{
		"aa:bb:cc:dd:ee:ff": "aabbccddeeff",
		"AA-BB-CC-DD-EE-FF": "aabbccddeeff",
		"aabb.ccdd.eeff":    "aabbccddeeff",
	}
	for in, want := range valid {
		if got := normalizeMAC(in); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "xyz", "aa:bb:cc", "aabbccddeeff00"} {
		if got := normalizeMAC(bad); got != "" {
			t.Errorf("normalizeMAC(%q) = %q, want empty", bad, got)
		}
	}
}

func TestRegisterAndHeartbeat(t *testing.T) {
	s := newTestState(t)
	now := time.Now()

	c, created, err := s.Register("PC001", "aa:bb:cc:dd:ee:01", now)
	if err != nil || !created {
		t.Fatalf("register: created=%v err=%v", created, err)
	}
	if c.Token == "" {
		t.Fatal("expected a client token")
	}

	// Re-register same MAC returns same client, no duplicate.
	c2, created2, err := s.Register("PC001-renamed", "AA:BB:CC:DD:EE:01", now)
	if err != nil || created2 {
		t.Fatalf("re-register should not create: created=%v err=%v", created2, err)
	}
	if c2.ID != c.ID {
		t.Errorf("same MAC gave different id: %s vs %s", c2.ID, c.ID)
	}
	if len(s.Clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(s.Clients))
	}

	// Heartbeat with a valid token updates last-seen.
	later := now.Add(10 * time.Second)
	got, ok, err := s.Heartbeat(c.Token, later)
	if err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	if !got.LastSeen.Equal(later) {
		t.Errorf("last-seen not updated: %v", got.LastSeen)
	}

	// Unknown token fails.
	if _, ok, _ := s.Heartbeat("bogus", later); ok {
		t.Error("unknown token must not heartbeat")
	}
}

func TestRegisterInvalidMAC(t *testing.T) {
	s := newTestState(t)
	if _, _, err := s.Register("PC", "not-a-mac", time.Now()); err == nil {
		t.Error("invalid MAC must be rejected")
	}
}

func TestOnlineOffline(t *testing.T) {
	s := newTestState(t)
	now := time.Now()
	c, _, _ := s.Register("PC", "aa:bb:cc:dd:ee:02", now)
	if !s.onlineLocked(c, now) {
		t.Error("fresh client should be online")
	}
	if s.onlineLocked(c, now.Add(200*time.Second)) {
		t.Error("stale client should be offline")
	}
}

func TestLockUnlock(t *testing.T) {
	s := newTestState(t)
	now := time.Now()
	c, _, _ := s.Register("PC", "aa:bb:cc:dd:ee:03", now)

	st, err := s.SetLock(c.ID, true, "admin")
	if err != nil || !st.Locked {
		t.Fatalf("lock: %+v err=%v", st, err)
	}
	st, err = s.SetLock(c.ID, false, "admin")
	if err != nil || st.Locked {
		t.Fatalf("unlock: %+v err=%v", st, err)
	}
	if _, err := s.SetLock("nope", true, "x"); err == nil {
		t.Error("locking unknown client must error")
	}
}

func TestSessionTimer(t *testing.T) {
	s := newTestState(t)
	now := time.Now()
	c, _, _ := s.Register("PC", "aa:bb:cc:dd:ee:04", now)

	st, err := s.StartSession(c.ID, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if st.Remaining(now) != 30*time.Minute {
		t.Errorf("remaining = %v, want 30m", st.Remaining(now))
	}
	if st.Expired(now.Add(29 * time.Minute)) {
		t.Error("should not be expired before deadline")
	}
	if !st.Expired(now.Add(31 * time.Minute)) {
		t.Error("should be expired after deadline")
	}
	if st.Remaining(now.Add(31*time.Minute)) != 0 {
		t.Error("remaining should clamp to 0")
	}

	// Invalid duration rejected.
	if _, err := s.StartSession(c.ID, 0, now); err == nil {
		t.Error("zero duration must be rejected")
	}

	// End clears the timer.
	st, err = s.EndSession(c.ID)
	if err != nil || st.SessionExpiresAt != nil {
		t.Fatalf("end session: %+v err=%v", st, err)
	}
}

func TestStatePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	now := time.Now()

	s1, err := newState(path, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := s1.Register("PC-Persist", "aa:bb:cc:dd:ee:05", now)
	_, _ = s1.SetLock(c.ID, true, "persisted")
	_, _ = s1.StartSession(c.ID, 15*time.Minute, now)

	// Reload from disk.
	s2, err := newState(path, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.ClientByToken(c.Token)
	if !ok || got.Name != "PC-Persist" {
		t.Fatalf("client not persisted: %+v ok=%v", got, ok)
	}
	st, ok := s2.GetState(c.ID)
	if !ok || !st.Locked || st.SessionExpiresAt == nil {
		t.Fatalf("state not persisted: %+v ok=%v", st, ok)
	}
}
