package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Register creates (or returns) a client for the given name and MAC. A MAC
// that already exists returns the existing client without creating a
// duplicate. The client token is server-issued and returned to the caller;
// it is persisted so the client can re-present it on heartbeat.
func (s *state) Register(name, mac string, now time.Time) (*Client, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	norm := normalizeMAC(mac)
	if norm == "" {
		return nil, false, errors.New("invalid MAC address")
	}
	if id, ok := s.byMAC[norm]; ok {
		c := s.Clients[id]
		if name != "" {
			c.Name = name
		}
		c.LastSeen = now
		if err := s.persistLocked(); err != nil {
			return nil, false, err
		}
		return c, false, nil
	}
	s.seq++
	tok, err := generateToken(24)
	if err != nil {
		return nil, false, err
	}
	id := fmt.Sprintf("client-%04d", s.seq)
	c := &Client{ID: id, Name: name, MAC: norm, Token: tok, LastSeen: now, CreatedAt: now}
	s.Clients[id] = c
	s.byMAC[norm] = id
	s.byToken[tok] = id
	if s.States[id] == nil {
		s.States[id] = &ClientState{ClientID: id}
	}
	if err := s.persistLocked(); err != nil {
		return nil, false, err
	}
	return c, true, nil
}

// Heartbeat updates a client's last-seen time. Returns ok=false if the token
// is unknown.
func (s *state) Heartbeat(token string, now time.Time) (*Client, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byToken[token]
	if !ok {
		return nil, false, nil
	}
	c := s.Clients[id]
	c.LastSeen = now
	if err := s.persistLocked(); err != nil {
		return nil, false, err
	}
	return c, true, nil
}

// GetState returns a copy of the authoritative state for a client.
func (s *state) GetState(id string) (ClientState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.States[id]
	if !ok {
		return ClientState{}, false
	}
	return *st, true
}

// SetLock sets the authoritative lock state for a client.
func (s *state) SetLock(id string, locked bool, reason string) (ClientState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Clients[id]; !ok {
		return ClientState{}, fmt.Errorf("unknown client %q", id)
	}
	st := s.States[id]
	if st == nil {
		st = &ClientState{ClientID: id}
		s.States[id] = st
	}
	st.Locked = locked
	st.LockReason = reason
	if err := s.persistLocked(); err != nil {
		return ClientState{}, err
	}
	return *st, nil
}

// StartSession sets a server-side session timer: the session expires duration
// from now. The expiry is stored as an absolute server time so a client
// cannot extend it by changing its own clock.
func (s *state) StartSession(id string, duration time.Duration, now time.Time) (ClientState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Clients[id]; !ok {
		return ClientState{}, fmt.Errorf("unknown client %q", id)
	}
	if duration <= 0 {
		return ClientState{}, errors.New("session duration must be positive")
	}
	st := s.States[id]
	if st == nil {
		st = &ClientState{ClientID: id}
		s.States[id] = st
	}
	exp := now.Add(duration)
	st.SessionStartedAt = now
	st.SessionExpiresAt = &exp
	if err := s.persistLocked(); err != nil {
		return ClientState{}, err
	}
	return *st, nil
}

// EndSession clears the session timer for a client.
func (s *state) EndSession(id string) (ClientState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Clients[id]; !ok {
		return ClientState{}, fmt.Errorf("unknown client %q", id)
	}
	st := s.States[id]
	if st == nil {
		st = &ClientState{ClientID: id}
		s.States[id] = st
	}
	st.SessionStartedAt = time.Time{}
	st.SessionExpiresAt = nil
	if err := s.persistLocked(); err != nil {
		return ClientState{}, err
	}
	return *st, nil
}

// Expired reports whether an active session has expired as of now. It does
// not mutate state; the evaluator decides what to do.
func (st ClientState) Expired(now time.Time) bool {
	return st.SessionExpiresAt != nil && now.After(*st.SessionExpiresAt)
}

// Remaining returns the session time left as of now (0 if none/expired).
func (st ClientState) Remaining(now time.Time) time.Duration {
	if st.SessionExpiresAt == nil {
		return 0
	}
	if d := st.SessionExpiresAt.Sub(now); d > 0 {
		return d
	}
	return 0
}

// normalizeMAC lower-cases and strips separators from a MAC address, keeping
// only hex digits. Returns "" if the result is not exactly 12 hex digits.
func normalizeMAC(mac string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(mac) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) != 12 {
		return ""
	}
	// Reject non-hex that slipped through (already constrained above).
	if _, err := strconv.ParseUint(out, 16, 64); err != nil {
		return ""
	}
	return out
}

// idSeq extracts the numeric suffix from a client id like "client-0007".
func idSeq(id string) int {
	i := strings.LastIndex(id, "-")
	if i < 0 || i == len(id)-1 {
		return 0
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return 0
	}
	return n
}
