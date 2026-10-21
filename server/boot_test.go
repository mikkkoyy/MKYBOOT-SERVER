package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBootScriptEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/bootscript", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#!ipxe") {
		t.Errorf("script must start with #!ipxe, got %q", strings.SplitN(body, "\n", 2)[0])
	}
	if !strings.Contains(body, "/api/bootstate") {
		t.Error("script does not chain to /api/bootstate")
	}
	// The api_base must point back at the request Host.
	if !strings.Contains(body, "http://example.com") {
		t.Errorf("script does not target request host; body:\n%s", body)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff header")
	}
	// Method enforcement.
	if do(t, srv, http.MethodPost, "/api/bootscript", "", "").Code != http.StatusMethodNotAllowed {
		t.Error("POST /api/bootscript should be 405")
	}
}

func TestBootStateUnknownClient(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/bootstate?mac=ff:ff:ff:ff:ff:ff", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["registered"] != false {
		t.Errorf("registered = %v, want false", resp["registered"])
	}
	if resp["boot_action"] != "continue" {
		t.Errorf("boot_action = %v, want continue", resp["boot_action"])
	}
}

func TestBootStateLockedClientHolds(t *testing.T) {
	srv, _ := newTestServer(t)
	reg := registerClient(t, srv, "BOOT-PC", "aa:bb:cc:00:00:10")

	// Lock the client directly in state (mirrors admin lock).
	st, ok := srv.state.GetState(reg.ClientID)
	if !ok {
		t.Fatal("state not found")
	}
	st.Locked = true
	st.LockReason = "session ended"
	srv.state.States[reg.ClientID] = &st
	_ = srv.state.persistLocked()

	// Query by MAC as iPXE would.
	rec := do(t, srv, http.MethodGet, "/api/bootstate?mac=aa:bb:cc:00:00:10", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["registered"] != true {
		t.Errorf("registered = %v, want true", resp["registered"])
	}
	if resp["boot_action"] != "hold" {
		t.Errorf("boot_action = %v, want hold for locked client", resp["boot_action"])
	}
}

func TestBootStateUnlockedContinues(t *testing.T) {
	srv, _ := newTestServer(t)
	reg := registerClient(t, srv, "BOOT-PC2", "aa:bb:cc:00:00:11")

	rec := do(t, srv, http.MethodGet, "/api/bootstate?mac=aa:bb:cc:00:00:11", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["registered"] != true || resp["boot_action"] != "continue" {
		t.Errorf("registered=%v action=%v, want true/continue", resp["registered"], resp["boot_action"])
	}
	_ = reg
}

func TestBootStateMethodEnforced(t *testing.T) {
	srv, _ := newTestServer(t)
	if do(t, srv, http.MethodPost, "/api/bootstate", "", "").Code != http.StatusMethodNotAllowed {
		t.Error("POST /api/bootstate should be 405")
	}
}

func TestBootScriptParamsValidation(t *testing.T) {
	bad := []BootScriptParams{
		{APIBase: "ftp://x"},
		{APIBase: "not-a-url"},
		{APIBase: "http://bad host"},
	}
	for _, p := range bad {
		if err := p.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error", p)
		}
	}
	good := BootScriptParams{APIBase: "http://198.51.100.2:8888"}
	if err := good.Validate(); err != nil {
		t.Errorf("valid params rejected: %v", err)
	}
	script, err := good.RenderIPXEScript()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.HasPrefix(script, "#!ipxe") || !strings.Contains(script, "198.51.100.2:8888") {
		t.Errorf("script missing header or target:\n%s", script)
	}
}
