package main

import (
	"net/http"
	"strings"
	"time"
)

// handleRoot gives a tiny unauthenticated banner so an operator hitting the
// base URL knows the server is alive without leaking admin data. It also
// serves GET /?api=status in the shape the existing Windows launcher already
// parses, so the launcher shows this server as reachable without any launcher
// change. Diskless services (DHCP/TFTP/iSCSI) are reported false in Phase 2
// because they are not implemented yet.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.URL.Query().Get("api") == "status" {
		s.handleCompatStatus(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "mkyboot-server",
		"version": serverVersion,
		"health":  "/healthz",
		"admin":   "/api/clients (requires Bearer admin token)",
	})
}

// handleCompatStatus mirrors the legacy /?api=status contract consumed by the
// existing Windows launcher. Authenticated callers get the full status;
// unauthenticated callers get {"error":"Not authenticated"}, which the
// launcher already interprets as "server reachable, sign-in required".
func (s *Server) handleCompatStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if bearerToken(r) == "" || !s.admin.Verify(bearerToken(r)) {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Not authenticated"})
		return
	}
	now := time.Now()
	views := s.state.List(now)
	online := 0
	for _, v := range views {
		if v.Online {
			online++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server":  map[string]any{"ipv4": s.cfg.BindAddress, "version": serverVersion, "vendor": "MKYBOOT"},
		"clients": map[string]any{"total": len(views), "online": online, "offline": len(views) - online},
		// Phase 2 does not run any diskless service yet.
		"services": map[string]any{"dhcp": false, "tftp": false, "iscsi": false},
	})
}

// handleHealth is the unauthenticated liveness endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "mkyboot-server",
		"version": serverVersion,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// registerRequest is the client registration body.
type registerRequest struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
}

// registerResponse returns the issued client credentials. The token is shown
// only here.
type registerResponse struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	Token    string `json:"token"`
	Created  bool   `json:"created"`
}

// handleClients dispatches GET (admin list) and POST (registration).
//
// GET requires the admin token. POST registration does NOT require the admin
// token: a workstation self-registers and receives a per-client token. This
// mirrors the lab model where clients enroll themselves; a deployment that
// needs gated enrollment can front this with network isolation.
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAdmin(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"clients": s.state.List(time.Now()),
			"count":   len(s.state.Clients),
		})
	case http.MethodPost:
		s.handleRegister(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Name) > 64 {
		writeError(w, http.StatusBadRequest, "name too long")
		return
	}
	if normalizeMAC(req.MAC) == "" {
		writeError(w, http.StatusBadRequest, "invalid MAC address")
		return
	}
	c, created, err := s.state.Register(req.Name, req.MAC, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.log.Info("client registered", map[string]any{"id": c.ID, "mac": c.MAC, "created": created})
	writeJSON(w, http.StatusOK, registerResponse{
		ClientID: c.ID, Name: c.Name, MAC: c.MAC, Token: c.Token, Created: created,
	})
}

// heartbeatRequest carries the client token.
type heartbeatRequest struct {
	Token string `json:"token"`
}

// handleHeartbeat updates last-seen for a client and returns its authoritative
// lock state and session timer so the client can enforce them.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req heartbeatRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	c, ok, err := s.state.Heartbeat(req.Token, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "unknown client token")
		return
	}
	st, _ := s.state.GetState(c.ID)
	// Authoritative expiry enforcement: if a session timer has elapsed, the
	// server reports the client as locked. The decision is made here from
	// server time, never from any client-supplied clock.
	if st.Expired(now) && !st.Locked {
		if st2, err := s.state.SetLock(c.ID, true, "session expired"); err == nil {
			st = st2
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":          c.ID,
		"online":             true,
		"locked":             st.Locked,
		"lock_reason":        st.LockReason,
		"session_remaining":  int(st.Remaining(now).Seconds()),
		"session_expires_at": st.SessionExpiresAt,
		"server_time":        now.UTC().Format(time.RFC3339),
	})
}
