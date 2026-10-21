package main

import (
	"errors"
	"fmt"
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
		Images:      len(s.storage.ListImages()),
		Assignments: len(s.storage.ListAssignments()),
		ISCSIState:  s.storage.iSCSIState().String(),
	}
	var missing []string
	if !cap.VHDX {
		missing = append(missing, "vhdx")
	}
	if !cap.Differencing {
		missing = append(missing, "differencing")
	}
	if !cap.BlockIsolation {
		missing = append(missing, "block-isolation")
	}
	if !cap.ISCSI {
		missing = append(missing, "iscsi")
	}
	if len(missing) > 0 {
		health.Message = "storage subsystem online; capabilities disabled: " + strings.Join(missing, ", ")
	} else {
		health.Message = "storage subsystem online"
	}
	writeJSON(w, http.StatusOK, health)
}

// handleStorageImages serves:
//
//	GET    /api/storage/images          list images
//	POST   /api/storage/images          create a new image
//	GET    /api/storage/images/{id}     get a single image
//	DELETE /api/storage/images/{id}     remove an image
//	GET    /api/storage/images/{id}/inspect  inspect image metadata
//
// Admin only.
func (s *Server) handleStorageImages(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/storage/images":
		switch r.Method {
		case http.MethodGet:
			imgs := s.storage.ListImages()
			writeJSON(w, http.StatusOK, map[string]any{"images": imgs, "count": len(imgs)})
		case http.MethodPost:
			s.handleStorageImageCreate(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	case strings.HasPrefix(path, "/api/storage/images/"):
		rest := strings.TrimPrefix(path, "/api/storage/images/")
		if rest == "" || strings.Contains(rest, "//") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		parts := strings.Split(rest, "/")
		switch len(parts) {
		case 1:
			id := parts[0]
			if id == "" {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				img, ok := s.storage.GetImage(id)
				if !ok {
					writeError(w, http.StatusNotFound, "image not found")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"image": img})
			case http.MethodDelete:
				if err := s.storage.RemoveImage(id); err != nil {
					if errors.Is(err, errImageNotFound) {
						writeError(w, http.StatusNotFound, err.Error())
						return
					}
					if errors.Is(err, errImageInUse) || errors.Is(err, errImageAttached) {
						writeError(w, http.StatusConflict, err.Error())
						return
					}
					if errors.Is(err, errBackendRequired) {
						writeError(w, http.StatusServiceUnavailable, err.Error())
						return
					}
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"id": id, "removed": true})
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case 2:
			id, sub := parts[0], parts[1]
			if id == "" || sub == "" {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			if r.Method != http.MethodGet || sub != "inspect" {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			img, vinfo, err := s.storage.InspectImage(id)
			if err != nil {
				if errors.Is(err, errImageNotFound) {
					writeError(w, http.StatusNotFound, err.Error())
					return
				}
				if errors.Is(err, errBackendRequired) {
					writeError(w, http.StatusServiceUnavailable, err.Error())
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"image": img, "inspect": vinfo})
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handleStorageImageCreate creates a new VHDX base image.
// POST /api/storage/images with body:
//
//	{"id": "win10", "name": "Windows 10 Base", "path": "images/win10.vhdx", "format": "vhdx", "size_mb": 40960}
func (s *Server) handleStorageImageCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Path     string `json:"path"`
		Format   string `json:"format"`
		SizeMB   int64  `json:"size_mb"`
		ParentID string `json:"parent_id,omitempty"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var format ImageFormat
	switch strings.ToLower(req.Format) {
	case "vhdx":
		format = ImageFormatVHDX
	case "vhd":
		format = ImageFormatVHD
	case "raw":
		format = ImageFormatRAW
	default:
		writeError(w, http.StatusBadRequest, "format must be vhdx, vhd, or raw")
		return
	}

	if (req.SizeMB < minImageSizeMB || req.SizeMB > maxImageSizeMB) && req.ParentID == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("size_mb must be between %d and %d", minImageSizeMB, maxImageSizeMB))
		return
	}

	var parentPath string
	if req.ParentID != "" {
		parentImg, ok := s.storage.GetImage(req.ParentID)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("parent image %q not found", req.ParentID))
			return
		}
		parentPath = parentImg.Path
	}

	img, err := s.storage.CreateImage(req.ID, req.Name, req.Path, format, req.SizeMB, parentPath)
	if err != nil {
		if errors.Is(err, errImageAlreadyExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, errBackendRequired) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"image": img})
}

// handleStorageAssign serves POST /api/storage/assign (assign) and
// DELETE /api/storage/assign/{clientID} (release, preserves overlay). Admin only.
// DELETE /api/storage/assign/{clientID}/overlay (discard, deletes overlay). Admin only.
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
		if strings.HasPrefix(path, "/api/storage/assign/") {
			rest := strings.TrimPrefix(path, "/api/storage/assign/")
			if rest == "" {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			// Check for /api/storage/assign/{clientID}/overlay (discard, deletes overlay).
			if strings.HasSuffix(rest, "/overlay") {
				clientID := strings.TrimSuffix(rest, "/overlay")
				if clientID == "" || strings.Contains(clientID, "/") {
					writeError(w, http.StatusNotFound, "not found")
					return
				}
				if err := s.storage.DiscardAssignment(clientID); err != nil {
					if errors.Is(err, errClientNotAssigned) {
						writeError(w, http.StatusNotFound, err.Error())
						return
					}
					if errors.Is(err, errBackendRequired) {
						writeError(w, http.StatusServiceUnavailable, err.Error())
						return
					}
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "discarded": true, "overlay_deleted": true})
				return
			}
			// Plain release: /api/storage/assign/{clientID} (preserves overlay).
			if strings.Contains(rest, "/") {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			clientID := rest
			if err := s.storage.ReleaseAssignment(clientID); err != nil {
				if errors.Is(err, errClientNotAssigned) {
					writeError(w, http.StatusNotFound, err.Error())
					return
				}
				if errors.Is(err, errBackendRequired) {
					writeError(w, http.StatusServiceUnavailable, err.Error())
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "released": true, "overlay_preserved": true})
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleISCSIHealth serves GET /api/storage/iscsi/health (admin only).
// Reports whether an iSCSI target backend is available and its verification state.
func (s *Server) handleISCSIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	state := s.storage.iSCSIState()
	info := map[string]any{
		"backend":   s.storage.iSCSIBackendName(),
		"available": s.storage.iSCSIBackendAvailable(),
		"state":     state.String(),
		"iscsi":     false, // capability flag remains false until verified
	}
	backend := s.storage.iSCSIBackend()
	if backend != nil && backend.IsAvailable() {
		targets, err := backend.ListTargets()
		if err != nil {
			info["error"] = err.Error()
		} else {
			info["targets"] = targets
		}
	}
	writeJSON(w, http.StatusOK, info)
}

// handleISCSIListTargets serves GET /api/storage/iscsi/targets (admin only).
// Lists currently exported iSCSI targets.
func (s *Server) handleISCSIListTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	targets, err := backend.ListTargets()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets, "count": len(targets)})
}

// handleISCSCreateLUN serves POST /api/storage/iscsi/luns (admin only).
// Creates a new block-backed LUN file. Body: {"path": "...", "size_mb": 1024}
func (s *Server) handleISCSCreateLUN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		Path   string `json:"path"`
		SizeMB int64  `json:"size_mb"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if req.SizeMB < 1 {
		writeError(w, http.StatusBadRequest, "size_mb must be positive")
		return
	}
	if err := backend.CreateLUN(req.Path, req.SizeMB); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": req.Path, "size_mb": req.SizeMB, "created": true})
}

// handleISCSCreateLUNDelete serves DELETE /api/storage/iscsi/luns/ (admin only).
// Removes a LUN file. Body: {"path": "..."}
func (s *Server) handleISCSCreateLUNDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := backend.RemoveLUN(req.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": req.Path, "removed": true})
}

// handleISCSIExport serves POST /api/storage/iscsi/export (admin only).
// Exports a LUN as an iSCSI target. Body: {"iqn": "...", "path": "...", "size_mb": 1024, "chap_user": "...", "chap_secret": "..."}
// CHAP secret is never logged.
func (s *Server) handleISCSIExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		IQN        string `json:"iqn"`
		Path       string `json:"path"`
		SizeMB     int64  `json:"size_mb"`
		CHAPUser   string `json:"chap_user,omitempty"`
		CHAPSecret string `json:"chap_secret,omitempty"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateIQN(req.IQN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := backend.ExportLUN(req.IQN, req.Path, req.SizeMB, req.CHAPUser, req.CHAPSecret); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"iqn": req.IQN, "path": req.Path, "exported": true})
}

// handleISCSIUnexport serves DELETE /api/storage/iscsi/export/ (admin only).
// Unexports an iSCSI target. Body: {"iqn": "..."}
func (s *Server) handleISCSIUnexport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		IQN string `json:"iqn"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateIQN(req.IQN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := backend.UnexportLUN(req.IQN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"iqn": req.IQN, "unexported": true})
}

// handleISCSISetACL serves POST /api/storage/iscsi/acl (admin only).
// Sets initiator ACL for a target. Body: {"iqn": "...", "initiators": ["iqn:..."]}
func (s *Server) handleISCSISetACL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		IQN        string   `json:"iqn"`
		Initiators []string `json:"initiators"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateIQN(req.IQN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := backend.SetACL(req.IQN, req.Initiators); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"iqn": req.IQN, "acl_updated": true})
}

// handleISCSIVerify serves POST /api/storage/iscsi/verify (admin only).
// Attempts a loopback connection to verify the target is functional.
// Body: {"iqn": "..."}
func (s *Server) handleISCSIVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	backend := s.storage.iSCSIBackend()
	if backend == nil || !backend.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, ErriSCSIBackendUnavailable.Error())
		return
	}
	var req struct {
		IQN string `json:"iqn"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateIQN(req.IQN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := backend.VerifyConnection(req.IQN); err != nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("verification failed: %v", err))
		return
	}
	s.storage.setISCSIState(iSCSIStateVerified)
	writeJSON(w, http.StatusOK, map[string]any{"iqn": req.IQN, "verified": true})
}
