package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// maxBodyBytes bounds request bodies to prevent memory-exhaustion from a
// malicious or buggy client.
const maxBodyBytes = 64 << 10 // 64 KiB

// heartbeatOfflineAfter is how long a client may go without a heartbeat before
// it is considered offline.
const heartbeatOfflineAfter = 90 * time.Second

// Server wires configuration, persistent state, admin auth and the HTTP API.
type Server struct {
	cfg     Config
	state   *state
	admin   *adminStore
	storage *Storage
	log     *Logger
	http    *http.Server
	ln      net.Listener
}

// NewServer builds the server. It loads or initializes persistent state and
// ensures an administrator token is provisioned. If a token was generated it
// is returned so the caller can display it exactly once; the caller is
// responsible for that display (never logged here).
func NewServer(cfg Config, log *Logger) (*Server, string, error) {
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	st, err := newState(DefaultStatePath(), heartbeatOfflineAfter)
	if err != nil {
		return nil, "", err
	}
	admin := newAdminStore(defaultAdminTokenPath())
	token, _, _, err := admin.Provision()
	if err != nil {
		return nil, "", fmt.Errorf("provision admin token: %w", err)
	}

	// Initialize the storage subsystem. The root defaults to
	// <data-root>/images when not explicitly configured.
	storageRoot := cfg.Storage.Root
	if storageRoot == "" {
		storageRoot = filepath.Join(ProgramDataRoot(), "images")
	}
	stg, err := NewStorage(storageRoot, log, cfg.Storage.ISCSI)
	if err != nil {
		return nil, "", fmt.Errorf("storage init: %w", err)
	}

	s := &Server{cfg: cfg, state: st, admin: admin, storage: stg, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/clients", s.handleClients) // GET list, POST register
	mux.HandleFunc("/api/clients/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/api/client/", s.handleClientItem) // /api/client/{id}/...
	mux.HandleFunc("/api/session/", s.handleSession)   // /api/session/{id}
	mux.HandleFunc("/api/bootstate", s.handleBootState)
	mux.HandleFunc("/api/bootscript", s.handleBootScript)
	mux.HandleFunc("/api/storage/health", s.handleStorageHealth)
	mux.HandleFunc("/api/storage/images", s.handleStorageImages)            // GET list, POST create
	mux.HandleFunc("/api/storage/images/", s.handleStorageImages)           // GET {id}, DELETE {id}, GET {id}/inspect
	mux.HandleFunc("/api/storage/assign", s.handleStorageAssign)            // POST assign
	mux.HandleFunc("/api/storage/assign/", s.handleStorageAssign)           // DELETE /api/storage/assign/{clientID}
	mux.HandleFunc("/api/storage/iscsi/health", s.handleISCSIHealth)        // GET iSCSI backend state
	mux.HandleFunc("/api/storage/iscsi/targets", s.handleISCSIListTargets)  // GET list targets
	mux.HandleFunc("/api/storage/iscsi/luns", s.handleISCSCreateLUN)        // POST create LUN
	mux.HandleFunc("/api/storage/iscsi/luns/", s.handleISCSCreateLUNDelete) // DELETE remove LUN
	mux.HandleFunc("/api/storage/iscsi/export", s.handleISCSIExport)        // POST export LUN as target
	mux.HandleFunc("/api/storage/iscsi/export/", s.handleISCSIUnexport)     // DELETE unexport target
	mux.HandleFunc("/api/storage/iscsi/acl", s.handleISCSISetACL)           // POST set initiator ACL
	mux.HandleFunc("/api/storage/iscsi/verify", s.handleISCSIVerify)        // POST verify target connection
	mux.HandleFunc("/", s.handleRoot)

	s.http = &http.Server{
		Handler:           s.recoverMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, token, nil
}

// defaultAdminTokenPath is the admin credential file under the data root.
func defaultAdminTokenPath() string { return filepath.Join(ProgramDataRoot(), "admin.token.json") }

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.cfg.Addr() }

// Listen binds the configured address. Binding failures are returned as
// actionable errors (e.g. port already in use).
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.cfg.Addr())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Addr(), err)
	}
	s.ln = ln
	return nil
}

// Serve accepts connections until the context is cancelled, then performs a
// graceful shutdown. It is safe to call only after Listen.
func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		if err := s.Listen(); err != nil {
			return err
		}
	}
	errCh := make(chan error, 1)
	go func() {
		if err := s.http.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.Shutdown()
	}
}

// Shutdown gracefully stops the HTTP server with a bounded drain timeout.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// recoverMiddleware converts a panic in any handler into a 500 instead of
// crashing the service.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("handler panic", map[string]any{"path": r.URL.Path, "panic": fmt.Sprint(rec)})
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requireAdmin enforces the bearer admin token on administrative endpoints.
// It uses a constant-time comparison and returns false (after writing the
// response) when unauthorized.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	tok := bearerToken(r)
	if tok == "" || !s.admin.Verify(tok) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

// bearerToken extracts a token from the Authorization: Bearer header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads and decodes a bounded JSON body into dst.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
