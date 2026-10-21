package main

import (
	"net/http"
	"time"
)

// handleBootState is the lightweight endpoint the generated iPXE script chains
// to. It is unauthenticated (boot clients have no token yet) and returns only
// non-sensitive session state for a client identified by MAC. It never exposes
// the admin token or client credentials.
func (s *Server) handleBootState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mac := normalizeMAC(r.URL.Query().Get("mac"))
	now := time.Now()
	resp := map[string]any{
		"server":      "mkyboot-server",
		"version":     serverVersion,
		"server_time": now.UTC().Format(time.RFC3339),
		"mac":         mac,
	}

	// Resolve the registered client by MAC (state stores MACs normalized).
	var id string
	if mac != "" {
		for _, v := range s.state.List(now) {
			if normalizeMAC(v.MAC) == mac {
				id = v.ID
				break
			}
		}
	}
	if id == "" {
		resp["registered"] = false
		resp["boot_action"] = "continue" // unknown client; let boot proceed
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp["registered"] = true
	st, ok := s.state.GetState(id)
	if !ok {
		resp["boot_action"] = "continue"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// A session that has elapsed forces a lock, mirroring heartbeat logic.
	locked := st.Locked
	reason := st.LockReason
	if st.Expired(now) {
		locked = true
		reason = "session expired"
	}
	resp["locked"] = locked
	resp["lock_reason"] = reason
	if st.SessionExpiresAt != nil {
		resp["session_expires_at"] = st.SessionExpiresAt.UTC().Format(time.RFC3339)
		resp["session_remaining"] = int(st.Remaining(now).Seconds())
	}
	// A locked client must not proceed to a diskless image.
	if locked {
		resp["boot_action"] = "hold"
	} else {
		resp["boot_action"] = "continue"
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleBootScript returns a downloadable iPXE boot script targeting this
// server. The api_base is derived from the request's Host header so the script
// points back at the address the client already reached.
func (s *Server) handleBootScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		host = s.Addr()
	}
	params := BootScriptParams{APIBase: scheme + "://" + host}
	script, err := params.RenderIPXEScript()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot render boot script")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}
