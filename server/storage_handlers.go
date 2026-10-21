package main

import (
	"errors"
	"net/http"
	"strings"
)

// handleStorageHealth serves GET /api/storage/health (admin only).
func (s *Server) handleStorageHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	cap := s.storage.Capabilities()
	health := StorageHealth{
		OK:          true,
		Message:     "storage subsystem online",
		Images:      len(s.storage.ListImages()),
		Assignments: len(s.storage.ListAssignments()),
	}
	if !cap.VHDX || !cap.Differencing || !cap.BlockIsolation || !cap.ISCSI {
		health.Message = "storage subsystem online; VHDX/differencing/block-isolation/iSCSI not yet implemented"
	}
	writeJSON(w, http.StatusOK, health)
}

// handleStorageImages serves GET /api/storage/images (list) and
// GET /api/storage/images/{id} (single). Admin only.
func (s *Server) handleStorageImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/storage/images":
		imgs := s.storage.ListImages()
		writeJSON(w, http.StatusOK, map[string]any{"images": imgs, "count": len(imgs)})
	case strings.HasPrefix(path, "/api/storage/images/"):
		id := strings.TrimPrefix(path, "/api/storage/images/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		img, ok := s.storage.GetImage(id)
		if !ok {
			writeError(w, http.StatusNotFound, "image not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"image": img})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handleStorageAssign serves POST /api/storage/assign (assign) and
// DELETE /api/storage/assign/{clientID} (release). Admin only.
func (s *Server) handleStorageAssign(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	path := r.URL.Path
	switch r.Method {
	case http.MethodPost:
		if path != "/api/storage/assign" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var req struct {
			ClientID string `json:"client_id"`
			ImageID  string `json:"image_id"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a, err := s.storage.AssignClient(req.ClientID, req.ImageID)
		if err != nil {
			if errors.Is(err, errImageNotFound) {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			if errors.Is(err, errClientAssigned) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"assignment": a})
	case http.MethodDelete:
		if !strings.HasPrefix(path, "/api/storage/assign/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		clientID := strings.TrimPrefix(path, "/api/storage/assign/")
		if clientID == "" || strings.Contains(clientID, "/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err := s.storage.ReleaseAssignment(clientID); err != nil {
			if errors.Is(err, errClientNotAssigned) {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "released": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
