package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer builds a server backed by a temp data dir (via MKYBOOT_DATA_DIR)
// and returns it plus the generated admin token.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MKYBOOT_DATA_DIR", dir)
	t.Setenv(adminTokenEnvVar, "")

	logger := NewLogger(io.Discard)
	srv, adminTok, err := NewServer(DefaultConfig(), logger)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if adminTok == "" {
		t.Fatal("expected a generated admin token on first run")
	}
	return srv, adminTok
}

// do issues a request through the server's handler stack.
func do(t *testing.T, srv *Server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader = http.NoBody
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthNoAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"status\":\"ok\"") {
		t.Errorf("health body = %s", rec.Body.String())
	}
}

func TestAdminListRequiresAuth(t *testing.T) {
	srv, adminTok := newTestServer(t)
	if rec := do(t, srv, http.MethodGet, "/api/clients", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token status = %d, want 401", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/clients", "wrongtoken", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token status = %d, want 401", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/clients", adminTok, ""); rec.Code != http.StatusOK {
		t.Errorf("admin token status = %d, want 200", rec.Code)
	}
}

func registerClient(t *testing.T, srv *Server, name, mac string) registerResponse {
	t.Helper()
	body, _ := json.Marshal(registerRequest{Name: name, MAC: mac})
	rec := do(t, srv, http.MethodPost, "/api/clients", "", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp registerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	return resp
}

func TestRegisterHeartbeatList(t *testing.T) {
	srv, adminTok := newTestServer(t)

	reg := registerClient(t, srv, "PC001", "aa:bb:cc:00:00:01")
	if reg.Token == "" || reg.ClientID == "" {
		t.Fatalf("register missing fields: %+v", reg)
	}

	rec := do(t, srv, http.MethodGet, "/api/clients", adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "PC001") {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}

	hbBody, _ := json.Marshal(heartbeatRequest{Token: reg.Token})
	rec = do(t, srv, http.MethodPost, "/api/clients/heartbeat", "", string(hbBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", rec.Code, rec.Body.String())
	}

	badBody, _ := json.Marshal(heartbeatRequest{Token: "nope"})
	if rec := do(t, srv, http.MethodPost, "/api/clients/heartbeat", "", string(badBody)); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad heartbeat status = %d, want 401", rec.Code)
	}
}

func TestRegisterInvalidInput(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(t, srv, http.MethodPost, "/api/clients", "", `{"mac":"aa:bb:cc:dd:ee:ff"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("missing name status = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/clients", "", `{"name":"x","mac":"zzz"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad mac status = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/clients", "", `{not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed json status = %d, want 400", rec.Code)
	}
}

// TestCompatStatusForLauncher verifies the legacy /?api=status contract the
// existing Windows launcher parses: unauthenticated -> "Not authenticated"
// (launcher shows reachable/sign-in required); authenticated -> full status.
func TestCompatStatusForLauncher(t *testing.T) {
	srv, adminTok := newTestServer(t)
	registerClient(t, srv, "PC-LAUNCH", "aa:bb:cc:00:00:07")

	// Unauthenticated: launcher treats this as reachable.
	rec := do(t, srv, http.MethodGet, "/?api=status", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Not authenticated") {
		t.Fatalf("unauth status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Authenticated: full status with MKYBOOT marker fields.
	rec = do(t, srv, http.MethodGet, "/?api=status", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"version"`, `"clients"`, `"total":1`, `"services"`} {
		if !strings.Contains(body, want) {
			t.Errorf("status body missing %q: %s", want, body)
		}
	}
}
