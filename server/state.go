package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Client is a registered diskless workstation.
type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MAC       string    `json:"mac"`
	Token     string    `json:"token"` // per-client auth token (server-issued)
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
}

// ClientState is the server-authoritative runtime state for a client. It is
// deliberately separate from the registration record so lock/timer state can
// be reset independently of identity.
type ClientState struct {
	ClientID string `json:"client_id"`
	// Locked is the authoritative lock/unlock decision. Clients must obey it;
	// a client cannot change it directly.
	Locked bool `json:"locked"`
	// LockReason is a short human-readable note (admin unlock/lock, expiry).
	LockReason string `json:"lock_reason,omitempty"`
	// SessionStartedAt / SessionExpiresAt define a server-side session timer.
	// A zero ExpiresAt means no active timer.
	SessionStartedAt time.Time  `json:"session_started_at,omitempty"`
	SessionExpiresAt *time.Time `json:"session_expires_at,omitempty"`
}

// state is the full persisted runtime state.
type state struct {
	mu      sync.Mutex
	path    string
	Clients map[string]*Client      `json:"clients"` // keyed by ID
	States  map[string]*ClientState `json:"states"`  // keyed by client ID
	byMAC   map[string]string       `json:"-"`       // MAC (normalized) -> ID
	byToken map[string]string       `json:"-"`       // token -> ID
	offline time.Duration           `json:"-"`       // heartbeat staleness threshold
	seq     int                     `json:"-"`       // id sequence
}

// newState loads persisted state from path or starts empty. offline is the
// heartbeat staleness threshold used to compute online/offline status.
func newState(path string, offline time.Duration) (*state, error) {
	s := &state{
		path:    path,
		Clients: map[string]*Client{},
		States:  map[string]*ClientState{},
		byMAC:   map[string]string{},
		byToken: map[string]string{},
		offline: offline,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Clients == nil {
		s.Clients = map[string]*Client{}
	}
	if s.States == nil {
		s.States = map[string]*ClientState{}
	}
	s.rebuildIndexes()
	return s, nil
}

func (s *state) rebuildIndexes() {
	s.byMAC = map[string]string{}
	s.byToken = map[string]string{}
	maxID := 0
	for id, c := range s.Clients {
		s.byMAC[normalizeMAC(c.MAC)] = id
		if c.Token != "" {
			s.byToken[c.Token] = id
		}
		if n := idSeq(id); n > maxID {
			maxID = n
		}
	}
	s.seq = maxID
}

// persistLocked writes state to disk atomically. Callers must hold s.mu.
func (s *state) persistLocked() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.path, append(data, '\n'), 0o600)
}

// clientView is a client plus its computed online status for API responses.
type clientView struct {
	Client
	Online bool `json:"online"`
}

// List returns a snapshot of clients with computed online status, sorted by ID.
func (s *state) List(now time.Time) []clientView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]clientView, 0, len(s.Clients))
	for _, c := range s.Clients {
		out = append(out, clientView{Client: *c, Online: s.onlineLocked(c, now)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *state) onlineLocked(c *Client, now time.Time) bool {
	return !c.LastSeen.IsZero() && now.Sub(c.LastSeen) <= s.offline
}

// ClientByToken returns the client for a token, if any.
func (s *state) ClientByToken(token string) (*Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byToken[token]
	if !ok {
		return nil, false
	}
	return s.Clients[id], true
}
