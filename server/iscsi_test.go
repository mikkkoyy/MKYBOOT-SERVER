package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---- Fake iSCSI target backend for tests ----

// FakeISCSIBackend implements iSCSITargetBackend for testing. It tracks
// created LUNs, exported targets, and ACLs in memory and creates real
// zero-length files so os.Stat works during tests.
type FakeISCSIBackend struct {
	mu      sync.Mutex
	luns    map[string]int64            // path -> size bytes
	targets map[string]*iSCSITargetInfo // iqn -> target info
	acls    map[string][]string         // iqn -> allowed initiator IQNs
	failOp  string                      // operation name to fail
	failErr error                       // error to return for the failing op
}

func newFakeISCSIBackend() *FakeISCSIBackend {
	return &FakeISCSIBackend{
		luns:    make(map[string]int64),
		targets: make(map[string]*iSCSITargetInfo),
		acls:    make(map[string][]string),
	}
}

func (b *FakeISCSIBackend) Name() string { return "fake-iscsi" }

func (b *FakeISCSIBackend) IsAvailable() bool { return true }

// fail sets an operation to return failErr. Empty string clears the failure.
func (b *FakeISCSIBackend) fail(op string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failOp = op
	b.failErr = err
}

func (b *FakeISCSIBackend) checkFail(op string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failOp == op {
		return b.failErr
	}
	return nil
}

func (b *FakeISCSIBackend) ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

func (b *FakeISCSIBackend) CreateLUN(path string, sizeMB int64) error {
	if err := b.checkFail("create"); err != nil {
		return err
	}
	if err := b.ensureDir(path); err != nil {
		return err
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return err
	}
	b.mu.Lock()
	b.luns[path] = sizeMB * 1024 * 1024
	b.mu.Unlock()
	return nil
}

func (b *FakeISCSIBackend) RemoveLUN(path string) error {
	if err := b.checkFail("remove"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Reject removal if the LUN is exported.
	for _, t := range b.targets {
		if t.LUNPath == path && t.Exported {
			return fmtError("lun %q is exported, unexport first", path)
		}
	}
	delete(b.luns, path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *FakeISCSIBackend) ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error {
	if err := b.checkFail("export"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.luns[path]; !exists {
		return fmtError("lun %q not found", path)
	}
	if _, exists := b.targets[iqn]; exists {
		return fmtError("target %q already exists", iqn)
	}
	b.targets[iqn] = &iSCSITargetInfo{
		IQN:         iqn,
		LUNPath:     path,
		SizeBytes:   sizeMB * 1024 * 1024,
		Exported:    true,
		Initiators:  nil,
		CHAPEnabled: chapUser != "",
		Verified:    false,
	}
	return nil
}

func (b *FakeISCSIBackend) UnexportLUN(iqn string) error {
	if err := b.checkFail("unexport"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.targets[iqn]
	if !ok {
		return fmtError("target %q not found", iqn)
	}
	t.Exported = false
	return nil
}

func (b *FakeISCSIBackend) ListTargets() ([]iSCSITargetInfo, error) {
	if err := b.checkFail("list"); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]iSCSITargetInfo, 0, len(b.targets))
	for _, t := range b.targets {
		out = append(out, *t)
	}
	return out, nil
}

func (b *FakeISCSIBackend) SetACL(iqn string, initiatorIQNs []string) error {
	if err := b.checkFail("acl"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.targets[iqn]; !ok {
		return fmtError("target %q not found", iqn)
	}
	b.acls[iqn] = initiatorIQNs
	return nil
}

func (b *FakeISCSIBackend) VerifyConnection(iqn string) error {
	if err := b.checkFail("verify"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.targets[iqn]
	if !ok {
		return fmtError("target %q not found", iqn)
	}
	if !t.Exported {
		return fmtError("target %q is not exported", iqn)
	}
	t.Verified = true
	return nil
}

func (b *FakeISCSIBackend) lunSize(path string) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.luns[path]
	return v, ok
}

func (b *FakeISCSIBackend) targetState(iqn string) (*iSCSITargetInfo, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.targets[iqn]
	if !ok {
		return nil, false
	}
	return t, true
}

func fmtError(format string, args ...any) error {
	return &fakeISCSIError{msg: fmt.Sprintf(format, args...)}
}

type fakeISCSIError struct {
	msg string
}

func (e *fakeISCSIError) Error() string { return e.msg }

// ---- Test helpers ----

func newTestStorageWithISCSIBackend(t *testing.T, backend iSCSITargetBackend) (*Storage, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "images")
	st, err := NewStorage(root, NewLogger(io.Discard), ISCSIConfig{})
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if backend != nil {
		st.mu.Lock()
		st.iscsiBackend = backend
		st.iscsiState = iSCSIStateConfigured
		st.mu.Unlock()
	} else {
		// Explicitly clear the auto-detected backend for nil tests.
		st.mu.Lock()
		st.iscsiBackend = nil
		st.iscsiState = iSCSIStateUnavailable
		st.mu.Unlock()
	}
	return st, root
}

// ---- Tests ----

func TestISCSIBackendDetected(t *testing.T) {
	st, _ := newTestStorageWithISCSIBackend(t, newFakeISCSIBackend())
	if !st.iSCSIBackendAvailable() {
		t.Error("iSCSI backend should be available")
	}
	if got := st.iSCSIBackendName(); got != "fake-iscsi" {
		t.Errorf("backend name = %q, want %q", got, "fake-iscsi")
	}
	if got := st.iSCSIState(); got != iSCSIStateConfigured {
		t.Errorf("state = %v, want Configured", got)
	}
}

func TestISCSIBackendStates(t *testing.T) {
	if got := iSCSIStateUnavailable.String(); got != "unavailable" {
		t.Errorf("state string = %q, want unavailable", got)
	}
	if got := iSCSIStateConfigured.String(); got != "configured" {
		t.Errorf("state string = %q, want configured", got)
	}
	if got := iSCSIStateVerified.String(); got != "verified" {
		t.Errorf("state string = %q, want verified", got)
	}
}

func TestISCSIValidateIQN(t *testing.T) {
	valid := []string{
		"iqn.2026-10.com.example:storage.disk1",
		"iqn.1993-08.org.debian:01:123456789",
	}
	for _, iqn := range valid {
		if err := validateIQN(iqn); err != nil {
			t.Errorf("validateIQN(%q) = %v, want ok", iqn, err)
		}
	}
	invalid := []string{"", "not-an-iqn", "iqn.2026-10", "x", "iqn..com.example"}
	for _, iqn := range invalid {
		if err := validateIQN(iqn); err == nil {
			t.Errorf("validateIQN(%q) = nil, want error", iqn)
		}
	}
}

func TestISCSCreateAndRemoveLUN(t *testing.T) {
	backend := newFakeISCSIBackend()
	_, root := newTestStorageWithISCSIBackend(t, backend)

	lunPath := filepath.Join(root, "lun1.img")
	if err := backend.CreateLUN(lunPath, 1024); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}
	if _, err := os.Stat(lunPath); err != nil {
		t.Fatalf("LUN file should exist: %v", err)
	}
	if size, ok := backend.lunSize(lunPath); !ok || size != 1024*1024*1024 {
		t.Errorf("LUN size = %d, %v, want %d", size, ok, 1024*1024*1024)
	}

	// Remove LUN.
	if err := backend.RemoveLUN(lunPath); err != nil {
		t.Fatalf("RemoveLUN: %v", err)
	}
	if _, err := os.Stat(lunPath); err == nil {
		t.Error("LUN file should be removed")
	}
}

func TestISCSIExportAndUnexport(t *testing.T) {
	backend := newFakeISCSIBackend()
	_, root := newTestStorageWithISCSIBackend(t, backend)

	lunPath := filepath.Join(root, "lun1.img")
	if err := backend.CreateLUN(lunPath, 1024); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.disk1"
	if err := backend.ExportLUN(iqn, lunPath, 1024, "", ""); err != nil {
		t.Fatalf("ExportLUN: %v", err)
	}

	targets, err := backend.ListTargets()
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(targets))
	}
	if targets[0].IQN != iqn || !targets[0].Exported {
		t.Errorf("target not properly exported: %+v", targets[0])
	}
	if targets[0].CHAPEnabled {
		t.Error("CHAP should be disabled when no chap_user")
	}

	// Unexport.
	if err := backend.UnexportLUN(iqn); err != nil {
		t.Fatalf("UnexportLUN: %v", err)
	}
	targets, _ = backend.ListTargets()
	if len(targets) != 1 || targets[0].Exported {
		t.Error("target should be unexported")
	}
}

func TestISCSIExportWithCHAP(t *testing.T) {
	backend := newFakeISCSIBackend()
	st, _ := newTestStorageWithISCSIBackend(t, backend)

	lunPath := filepath.Join(st.root, "lun-chap.img")
	if err := backend.CreateLUN(lunPath, 512); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.disk2"
	if err := backend.ExportLUN(iqn, lunPath, 512, "chapuser", "chapsecret"); err != nil {
		t.Fatalf("ExportLUN with CHAP: %v", err)
	}

	tgt, ok := backend.targetState(iqn)
	if !ok {
		t.Fatal("target not found")
	}
	if !tgt.CHAPEnabled {
		t.Error("CHAP should be enabled when chap_user is set")
	}
}

func TestISCSISetACL(t *testing.T) {
	backend := newFakeISCSIBackend()
	st, _ := newTestStorageWithISCSIBackend(t, backend)

	lunPath := filepath.Join(st.root, "lun-acl.img")
	if err := backend.CreateLUN(lunPath, 512); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.disk3"
	if err := backend.ExportLUN(iqn, lunPath, 512, "", ""); err != nil {
		t.Fatalf("ExportLUN: %v", err)
	}

	if err := backend.SetACL(iqn, []string{"iqn.2026-10.com.client:initiator1"}); err != nil {
		t.Fatalf("SetACL: %v", err)
	}

	if _, ok := backend.targetState(iqn); !ok {
		t.Fatal("target not found")
	}
	// Note: FakeISCSIBackend.SetACL stores ACLs separately; targetState doesn't reflect ACLs.
	// Verify via acls map.
	backend.mu.Lock()
	acl := backend.acls[iqn]
	backend.mu.Unlock()
	if len(acl) != 1 || acl[0] != "iqn.2026-10.com.client:initiator1" {
		t.Errorf("ACL = %v, want [iqn.2026-10.com.client:initiator1]", acl)
	}
}

func TestISCSIVerifyConnection(t *testing.T) {
	backend := newFakeISCSIBackend()
	st, _ := newTestStorageWithISCSIBackend(t, backend)

	lunPath := filepath.Join(st.root, "lun-verify.img")
	if err := backend.CreateLUN(lunPath, 512); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.disk4"
	if err := backend.ExportLUN(iqn, lunPath, 512, "", ""); err != nil {
		t.Fatalf("ExportLUN: %v", err)
	}

	if err := backend.VerifyConnection(iqn); err != nil {
		t.Fatalf("VerifyConnection: %v", err)
	}

	tgt, ok := backend.targetState(iqn)
	if !ok || !tgt.Verified {
		t.Error("target should be verified after VerifyConnection")
	}
	if st.iSCSIState() != iSCSIStateConfigured {
		// Storage-level state is only updated via the verify endpoint, not directly
		t.Log("storage-level iSCSI state unchanged (updated via endpoint)")
	}
}

func TestISCISBackendUnavailable(t *testing.T) {
	st, root := newTestStorageWithISCSIBackend(t, nil)
	if st.iSCSIBackendAvailable() {
		t.Error("iSCSI backend should not be available when nil")
	}
	if st.iSCSIBackendName() != "none" {
		t.Errorf("backend name = %q, want none", st.iSCSIBackendName())
	}
	if st.iSCSIState() != iSCSIStateUnavailable {
		t.Errorf("state = %v, want Unavailable", st.iSCSIState())
	}

	// Operations should fail without a backend.
	if be := st.iSCSIBackend(); be != nil {
		_, err := be.ListTargets()
		if err == nil {
			t.Error("ListTargets on nil backend should fail")
		}
	}
	_ = root
}

func TestISCISBackendFailure(t *testing.T) {
	backend := newFakeISCSIBackend()
	_, root := newTestStorageWithISCSIBackend(t, backend)

	backend.fail("export", &fakeISCSIError{msg: "targetcli error"})
	lunPath := filepath.Join(root, "lun-fail.img")
	if err := backend.CreateLUN(lunPath, 512); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}
	iqn := "iqn.2026-10.com.example:storage.fail"
	if err := backend.ExportLUN(iqn, lunPath, 512, "", ""); err == nil {
		t.Error("export should fail when backend fails")
	}
	// Clear failure.
	backend.fail("", nil)
	if err := backend.ExportLUN(iqn, lunPath, 512, "", ""); err != nil {
		t.Errorf("export after clearing failure: %v", err)
	}
}

// ---- HTTP endpoint tests ----

func newTestISCSIHTTPServer(t *testing.T, backend iSCSITargetBackend) (*Server, string) {
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
		t.Fatal("expected admin token")
	}
	if backend != nil {
		srv.storage.mu.Lock()
		srv.storage.iscsiBackend = backend
		srv.storage.iscsiState = iSCSIStateConfigured
		srv.storage.mu.Unlock()
	}
	return srv, adminTok
}

func TestISCSIHealthEndpoint(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	// Unauthorized.
	if rec := do(t, srv, http.MethodGet, "/api/storage/iscsi/health", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("iscsi health without auth = %d, want 401", rec.Code)
	}

	// Authorized.
	rec := do(t, srv, http.MethodGet, "/api/storage/iscsi/health", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("iscsi health status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["available"] != true {
		t.Errorf("available should be true, got %v", body["available"])
	}
	if body["state"] != "configured" {
		t.Errorf("state should be configured, got %v", body["state"])
	}
	if body["iscsi"] != false {
		t.Errorf("iscsi capability should be false, got %v", body["iscsi"])
	}
}

func TestISCSCreateLUNEndpoint(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	lunPath := filepath.Join(t.TempDir(), "lun-http.img")
	body, _ := json.Marshal(map[string]any{"path": lunPath, "size_mb": 1024})
	rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/luns", adminTok, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create LUN status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	// Unauthorized.
	rec = do(t, srv, http.MethodPost, "/api/storage/iscsi/luns", "", string(body))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("create LUN without auth = %d, want 401", rec.Code)
	}

	// Missing path.
	body, _ = json.Marshal(map[string]any{"size_mb": 1024})
	rec = do(t, srv, http.MethodPost, "/api/storage/iscsi/luns", adminTok, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create LUN missing path = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	// Malformed JSON.
	rec = do(t, srv, http.MethodPost, "/api/storage/iscsi/luns", adminTok, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create LUN malformed json = %d, want 400", rec.Code)
	}
}

func TestISCSIExportEndpoint(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	lunPath := filepath.Join(t.TempDir(), "lun-export.img")
	if err := backend.CreateLUN(lunPath, 1024); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.http1"
	body, _ := json.Marshal(map[string]any{
		"iqn":     iqn,
		"path":    lunPath,
		"size_mb": 1024,
	})
	rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/export", adminTok, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("export status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	// Invalid IQN.
	body, _ = json.Marshal(map[string]any{
		"iqn":     "not-an-iqn",
		"path":    lunPath,
		"size_mb": 1024,
	})
	rec = do(t, srv, http.MethodPost, "/api/storage/iscsi/export", adminTok, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("export invalid IQN = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	// Duplicate export.
	rec = do(t, srv, http.MethodPost, "/api/storage/iscsi/export", adminTok, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("export duplicate = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestISCSIVerifyEndpoint(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	lunPath := filepath.Join(t.TempDir(), "lun-verify.img")
	if err := backend.CreateLUN(lunPath, 1024); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	iqn := "iqn.2026-10.com.example:storage.verify1"
	body, _ := json.Marshal(map[string]any{"iqn": iqn, "path": lunPath, "size_mb": 1024})
	if rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/export", adminTok, string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("pre-export: %d body=%s", rec.Code, rec.Body.String())
	}

	// Unauthorized verify.
	if rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/verify", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("verify without auth = %d, want 401", rec.Code)
	}

	// Invalid IQN.
	body, _ = json.Marshal(map[string]any{"iqn": "bad-iqn"})
	if rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/verify", adminTok, string(body)); rec.Code != http.StatusBadRequest {
		t.Errorf("verify invalid IQN = %d, want 400", rec.Code)
	}

	// Successful verify.
	body, _ = json.Marshal(map[string]any{"iqn": iqn})
	rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/verify", adminTok, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestISCISEndpointMethodEnforcement(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	if rec := do(t, srv, http.MethodGet, "/api/storage/iscsi/luns", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET luns = %d, want 405", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/targets", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST targets = %d, want 405", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/storage/iscsi/export", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET export = %d, want 405", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/storage/iscsi/verify", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET verify = %d, want 405", rec.Code)
	}
}

func TestISCSIConcurrentExport(t *testing.T) {
	backend := newFakeISCSIBackend()
	srv, adminTok := newTestISCSIHTTPServer(t, backend)

	lunPath := filepath.Join(t.TempDir(), "lun-concurrent.img")
	if err := backend.CreateLUN(lunPath, 1024); err != nil {
		t.Fatalf("CreateLUN: %v", err)
	}

	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			iqn := "iqn.2026-10.com.example:storage.concurrent"
			body, _ := json.Marshal(map[string]any{"iqn": iqn, "path": lunPath, "size_mb": 1024})
			rec := do(t, srv, http.MethodPost, "/api/storage/iscsi/export", adminTok, string(body))
			done <- rec.Code
		}()
	}
	successes := 0
	for i := 0; i < 10; i++ {
		if code := <-done; code == http.StatusCreated {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful export of same LUN, got %d", successes)
	}
}

// ---- SSH backend logic tests ----

func TestValidateLinuxPath(t *testing.T) {
	valid := []string{
		"/mnt/iscsi/lun1.img",
		"/var/lib/target/store/disk0",
		"/srv/iscsi/images/base.vhdx",
	}
	for _, p := range valid {
		if !validateLinuxPath(p) {
			t.Errorf("validateLinuxPath(%q) = false, want true", p)
		}
	}
	invalid := []string{
		"",
		"relative/path.img",
		"/path/with/../traversal",
		"/path/with|pipe",
		"/path/with;rm",
		"/path/with$var",
		"/path/with`cmd`",
		"/path/with<script",
	}
	for _, p := range invalid {
		if validateLinuxPath(p) {
			t.Errorf("validateLinuxPath(%q) = true, want false", p)
		}
	}
}

func TestSafeTargetName(t *testing.T) {
	if got := safeTargetName("iqn.2026-10.com.example:storage.disk1"); got != "iqn.2026-10.com.example_storage.disk1" {
		t.Errorf("safeTargetName(iqn) = %q, want %q", got, "iqn.2026-10.com.example_storage.disk1")
	}
	if got := safeTargetName("/mnt/iscsi/lun1.img"); got != "_mnt_iscsi_lun1.img" {
		t.Errorf("safeTargetName(path) = %q, want %q", got, "_mnt_iscsi_lun1.img")
	}
	if got := safeTargetName(""); got == "" {
		t.Error("safeTargetName(empty) should not be empty")
	}
	// No shell metacharacters ever appear.
	for _, s := range []string{"a|b", "c;d", "e$f", "g`h`", "i<j", "k>l", "m&n", "o'p", "q\"r"} {
		name := safeTargetName(s)
		if strings.ContainsAny(name, "|;$(){}[]<>\"'`\\") {
			t.Errorf("safeTargetName(%q) = %q contains metacharacters", s, name)
		}
	}
}

func TestParseTargetcliTargets(t *testing.T) {
	out := `
/iscsi
    iqn.2026-10.com.example:storage.disk1
        tpg1
            luns
                lun0 -> /backstores/fileio/lun1
            acls
    iqn.2026-10.com.example:storage.disk2
        tpg1
            luns
                lun0 -> /backstores/fileio/lun2
`
	targets := parseTargetcliTargets(out)
	if len(targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].IQN != "iqn.2026-10.com.example:storage.disk1" {
		t.Errorf("target[0].IQN = %q, want %q", targets[0].IQN, "iqn.2026-10.com.example:storage.disk1")
	}
	if targets[1].IQN != "iqn.2026-10.com.example:storage.disk2" {
		t.Errorf("target[1].IQN = %q, want %q", targets[1].IQN, "iqn.2026-10.com.example:storage.disk2")
	}
	for _, tt := range targets {
		if !tt.Exported {
			t.Errorf("target %q should be exported", tt.IQN)
		}
	}
}

func TestParseTargetcliTargetsEmpty(t *testing.T) {
	targets := parseTargetcliTargets("")
	if len(targets) != 0 {
		t.Errorf("expected 0 targets, got %d", len(targets))
	}
}

func TestParseTargetcliACLs(t *testing.T) {
	out := `
    iqn.2026-10.com.client:initiator1
    iqn.2026-10.com.client:initiator2
`
	acls := parseTargetcliACLs(out)
	if len(acls) != 2 {
		t.Errorf("expected 2 ACLs, got %d", len(acls))
	}
	if acls[0] != "iqn.2026-10.com.client:initiator1" {
		t.Errorf("acl[0] = %q, want %q", acls[0], "iqn.2026-10.com.client:initiator1")
	}
}
