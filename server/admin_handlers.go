package main

import (
	"net/http"
	"strings"
	"time"
)

// serviceName is the Windows Service Control Manager service name. It is
// declared here (platform-neutral) so the CLI help/feedback text in main.go
// compiles on every OS; the install/uninstall operations themselves are
// Windows-only (see service_windows.go / service_other.go).
const serviceName = "MKYBOOTServer"

// serverVersion identifies this build in health/status responses.
const serverVersion = "0.1.0-phase2"

// handleClientItem serves /api/client/{id}/lock, /unlock and /state.
// All of these are administrative and require the admin token.
func (s *Server) handleClientItem(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	// Path: /api/client/{id}/{action}
	rest := strings.TrimPrefix(r.URL.Path, "/api/client/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, action := parts[0], parts[1]
	switch action {
	case "state":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		st, ok := s.state.GetState(id)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown client")
			return
		}
		now := time.Now()
		writeJSON(w, http.StatusOK, map[string]any{
			"client_id":          id,
			"locked":             st.Locked,
			"lock_reason":        st.LockReason,
			"session_remaining":  int(st.Remaining(now).Seconds()),
			"session_expires_at": st.SessionExpiresAt,
		})
	case "lock", "unlock":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		locked := action == "lock"
		reason := action
		st, err := s.state.SetLock(id, locked, reason)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.log.Info("lock state changed", map[string]any{"id": id, "locked": locked})
		writeJSON(w, http.StatusOK, map[string]any{"client_id": id, "locked": st.Locked, "lock_reason": st.LockReason})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handleSession serves /api/session/{id} for start (POST) and end (DELETE) of
// a server-authoritative session timer. Administrative only.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/session/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Minutes int `json:"minutes"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Minutes <= 0 || req.Minutes > 24*60 {
			writeError(w, http.StatusBadRequest, "minutes must be between 1 and 1440")
			return
		}
		st, err := s.state.StartSession(id, time.Duration(req.Minutes)*time.Minute, time.Now())
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.log.Info("session started", map[string]any{"id": id, "minutes": req.Minutes})
		writeJSON(w, http.StatusOK, map[string]any{
			"client_id":          id,
			"session_expires_at": st.SessionExpiresAt,
			"session_minutes":    req.Minutes,
		})
	case http.MethodDelete:
		st, err := s.state.EndSession(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.log.Info("session ended", map[string]any{"id": id})
		writeJSON(w, http.StatusOK, map[string]any{"client_id": id, "session_expires_at": st.SessionExpiresAt})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
