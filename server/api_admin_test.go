package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLockUnlockViaAPI(t *testing.T) {
	srv, adminTok := newTestServer(t)
	reg := registerClient(t, srv, "PC002", "aa:bb:cc:00:00:02")

	if rec := do(t, srv, http.MethodPost, "/api/client/"+reg.ClientID+"/lock", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("lock without auth = %d, want 401", rec.Code)
	}
	rec := do(t, srv, http.MethodPost, "/api/client/"+reg.ClientID+"/lock", adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"locked\":true") {
		t.Fatalf("lock status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodPost, "/api/client/"+reg.ClientID+"/unlock", adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "\"locked\":false") {
		t.Fatalf("unlock status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/client/client-9999/lock", adminTok, ""); rec.Code != http.StatusNotFound {
		t.Errorf("lock unknown = %d, want 404", rec.Code)
	}
}

func TestSessionTimerViaAPI(t *testing.T) {
	srv, adminTok := newTestServer(t)
	reg := registerClient(t, srv, "PC003", "aa:bb:cc:00:00:03")

	rec := do(t, srv, http.MethodPost, "/api/session/"+reg.ClientID, adminTok, `{"minutes":30}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start session status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/session/"+reg.ClientID, adminTok, `{"minutes":0}`); rec.Code != http.StatusBadRequest {
		t.Errorf("zero minutes = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/session/"+reg.ClientID, adminTok, ""); rec.Code != http.StatusOK {
		t.Errorf("end session = %d, want 200", rec.Code)
	}
}

// TestSessionExpiryForcesLock verifies the server-authoritative rule: once a
// session timer elapses, the next heartbeat reports the client locked.
func TestSessionExpiryForcesLock(t *testing.T) {
	srv, _ := newTestServer(t)
	reg := registerClient(t, srv, "PC004", "aa:bb:cc:00:00:04")

	st, err := srv.state.StartSession(reg.ClientID, time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	st.SessionExpiresAt = &past
	srv.state.States[reg.ClientID] = &st
	_ = srv.state.persistLocked()

	hbBody, _ := json.Marshal(heartbeatRequest{Token: reg.Token})
	rec := do(t, srv, http.MethodPost, "/api/clients/heartbeat", "", string(hbBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"locked\":true") {
		t.Errorf("expired session must force lock, body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "session expired") {
		t.Errorf("expected expiry reason, body=%s", rec.Body.String())
	}
}

// TestRequestBodyLimit ensures an oversized body is rejected, not buffered.
func TestRequestBodyLimit(t *testing.T) {
	srv, _ := newTestServer(t)
	big := bytes.Repeat([]byte("a"), maxBodyBytes+1024)
	rec := do(t, srv, http.MethodPost, "/api/clients", "", string(big))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized body status = %d, want 400", rec.Code)
	}
}

// TestPersistenceAcrossRestart verifies clients and state survive a server
// rebuild against the same data dir (simulates a service restart).
func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MKYBOOT_DATA_DIR", dir)
	t.Setenv(adminTokenEnvVar, "")

	srv1, adminTok, err := NewServer(DefaultConfig(), NewLogger(io.Discard))
	if err != nil {
		t.Fatalf("NewServer1: %v", err)
	}
	reg := registerClient(t, srv1, "PC-Persist", "aa:bb:cc:00:00:09")
	do(t, srv1, http.MethodPost, "/api/client/"+reg.ClientID+"/lock", adminTok, "")

	srv2, _, err := NewServer(DefaultConfig(), NewLogger(io.Discard))
	if err != nil {
		t.Fatalf("NewServer2: %v", err)
	}
	got, ok := srv2.state.ClientByToken(reg.Token)
	if !ok || got.Name != "PC-Persist" {
		t.Fatalf("client did not persist across restart: %+v ok=%v", got, ok)
	}
	st, _ := srv2.state.GetState(reg.ClientID)
	if !st.Locked {
		t.Error("lock state did not persist across restart")
	}
}

func TestDataDirFilePermissions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("owner-only DACL check is Windows-specific")
	}
	dir := t.TempDir()
	t.Setenv("MKYBOOT_DATA_DIR", dir)
	t.Setenv(adminTokenEnvVar, "")
	if _, _, err := NewServer(DefaultConfig(), NewLogger(io.Discard)); err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	tokenPath := filepath.Join(dir, "admin.token.json")
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatal(err)
	}
	ownerOnly, err := fileIsOwnerOnly(tokenPath)
	if err != nil {
		t.Fatalf("fileIsOwnerOnly: %v", err)
	}
	if !ownerOnly {
		t.Error("admin token file must be owner-only (protected DACL, no broad groups)")
	}
}
