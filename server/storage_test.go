package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "images")
	st, err := NewStorage(root, NewLogger(io.Discard), ISCSIConfig{})
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	// Tests use a fake backend so AssignClient does not invoke real DiskPart.
	fake := newFakeBackend()
	st.mu.Lock()
	st.backend = fake
	st.cap.VHDX = fake.IsAvailable()
	st.cap.Differencing = fake.IsAvailable()
	st.mu.Unlock()
	return st, root
}

func makeTestImage(t *testing.T, dir, name, ext string) string {
	t.Helper()
	path := filepath.Join(dir, name+ext)
	if err := os.WriteFile(path, []byte("test-image-data"), 0o600); err != nil {
		t.Fatalf("write test image: %v", err)
	}
	return path
}

func TestStorageCapabilitiesAllFalse(t *testing.T) {
	st, _ := newTestStorage(t)
	cap := st.Capabilities()
	// Capabilities depend on the detected backend. On Windows with DiskPart
	// available, VHDX and Differencing are true; block isolation and iSCSI
	// remain false. Without a backend, all are false.
	if cap.BlockIsolation {
		t.Error("BlockIsolation capability should be false")
	}
	if cap.ISCSI {
		t.Error("ISCSI capability should be false")
	}
}

func TestStorageRootIsAbsolute(t *testing.T) {
	st, root := newTestStorage(t)
	if got := st.Root(); !filepath.IsAbs(got) {
		t.Errorf("root %q is not absolute", got)
	}
	if got := st.Root(); got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestStoragePathContainment(t *testing.T) {
	st, root := newTestStorage(t)

	// Contained paths are OK.
	if !st.IsPathContained(filepath.Join(root, "base.vhdx")) {
		t.Error("contained path should be valid")
	}
	if !st.IsPathContained(filepath.Join(root, "sub", "base.vhdx")) {
		t.Error("nested contained path should be valid")
	}
	if !st.IsPathContained(root) {
		t.Error("root itself should be contained")
	}

	// Traversal escapes are rejected.
	if st.IsPathContained(filepath.Join(root, "..", "..", "escape.vhdx")) {
		t.Error("path traversal should be rejected")
	}
	if st.IsPathContained(filepath.Join(root, "sub", "..", "..", "escape.vhdx")) {
		t.Error("nested path traversal should be rejected")
	}

	// A sibling directory is not contained.
	if st.IsPathContained(filepath.Join(filepath.Dir(root), "sibling.vhdx")) {
		t.Error("sibling path should be rejected")
	}

	// An empty/relative path that resolves outside root is rejected.
	if st.IsPathContained("../escape.vhdx") {
		t.Error("relative traversal should be rejected")
	}
}

func TestStorageRegisterExisting(t *testing.T) {
	st, root := newTestStorage(t)

	imgPath := makeTestImage(t, root, "win10-base", ".vhdx")

	img, err := st.RegisterExisting("win10", "Windows 10 Base", imgPath)
	if err != nil {
		t.Fatalf("RegisterExisting: %v", err)
	}
	if img.ID != "win10" {
		t.Errorf("id = %q, want %q", img.ID, "win10")
	}
	if img.Name != "Windows 10 Base" {
		t.Errorf("name = %q, want %q", img.Name, "Windows 10 Base")
	}
	if img.Format != ImageFormatVHDX {
		t.Errorf("format = %v, want VHDX", img.Format)
	}
	if img.Status != ImageStatusReady {
		t.Errorf("status = %v, want Ready", img.Status)
	}
	if !img.ReadOnly {
		t.Error("read_only should be true")
	}
	if img.SizeBytes <= 0 {
		t.Error("size_bytes should be positive")
	}
	if img.Path != imgPath {
		t.Errorf("path = %q, want %q", img.Path, imgPath)
	}

	// Duplicate id is rejected.
	if _, err := st.RegisterExisting("win10", "dup", imgPath); err == nil {
		t.Error("duplicate image id should be rejected")
	}

	// Empty id is rejected.
	if _, err := st.RegisterExisting("", "empty", imgPath); err == nil {
		t.Error("empty image id should be rejected")
	}

	// Path outside root is rejected.
	outside := filepath.Join(filepath.Dir(root), "outside.vhdx")
	makeTestImage(t, filepath.Dir(root), "outside", ".vhdx")
	if _, err := st.RegisterExisting("outside", "outside", outside); err == nil {
		t.Error("image outside root should be rejected")
	}

	// Missing file is rejected.
	if _, err := st.RegisterExisting("missing", "missing", filepath.Join(root, "nope.vhdx")); err == nil {
		t.Error("missing image should be rejected")
	}

	// Directory instead of file is rejected.
	subdir := filepath.Join(root, "adir")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := st.RegisterExisting("adir", "dir", subdir); err == nil {
		t.Error("directory path should be rejected")
	}
}

func TestStorageGetAndListImages(t *testing.T) {
	st, root := newTestStorage(t)

	// Empty list.
	if imgs := st.ListImages(); len(imgs) != 0 {
		t.Errorf("expected 0 images, got %d", len(imgs))
	}
	if _, ok := st.GetImage("nonexistent"); ok {
		t.Error("nonexistent image should not be found")
	}

	// Register two images.
	p1 := makeTestImage(t, root, "img-a", ".vhdx")
	p2 := makeTestImage(t, root, "img-b", ".vhd")
	_, _ = st.RegisterExisting("img-a", "Image A", p1)
	_, _ = st.RegisterExisting("img-b", "Image B", p2)

	// Get single image.
	img, ok := st.GetImage("img-a")
	if !ok {
		t.Fatal("img-a not found")
	}
	if img.ID != "img-a" {
		t.Errorf("id = %q, want %q", img.ID, "img-a")
	}

	// List returns both, sorted by id.
	imgs := st.ListImages()
	if len(imgs) != 2 {
		t.Errorf("expected 2 images, got %d", len(imgs))
	}
	if imgs[0].ID != "img-a" || imgs[1].ID != "img-b" {
		t.Errorf("images not sorted: %+v", imgs)
	}
}

func TestStorageAssignAndRelease(t *testing.T) {
	st, root := newTestStorage(t)

	imgPath := makeTestImage(t, root, "base", ".vhdx")
	_, err := st.RegisterExisting("base-img", "Base Image", imgPath)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Assign client to image.
	a, err := st.AssignClient("client-1", "base-img")
	if err != nil {
		t.Fatalf("AssignClient: %v", err)
	}
	if a.ClientID != "client-1" {
		t.Errorf("client_id = %q, want %q", a.ClientID, "client-1")
	}
	if a.ImageID != "base-img" {
		t.Errorf("image_id = %q, want %q", a.ImageID, "base-img")
	}
	if !a.Writable {
		t.Error("writable should be true")
	}
	if a.AssignedAt.IsZero() {
		t.Error("assigned_at should be set")
	}

	// Duplicate assignment for same client is rejected.
	if _, err := st.AssignClient("client-1", "base-img"); err == nil {
		t.Error("duplicate assignment should be rejected")
	}
	if _, err := st.AssignClient("client-1", "base-img"); !strings.Contains(err.Error(), "already") {
		t.Errorf("duplicate assignment error should mention already: %v", err)
	}

	// Unknown image is rejected.
	if _, err := st.AssignClient("client-2", "nope"); err == nil {
		t.Error("unknown image assignment should be rejected")
	}

	// Get assignment.
	got, ok := st.GetAssignment("client-1")
	if !ok {
		t.Fatal("assignment not found")
	}
	if got.ClientID != "client-1" {
		t.Errorf("client_id = %q, want %q", got.ClientID, "client-1")
	}

	// Release assignment - overlay file must be preserved (data safety).
	overlayPath := filepath.Join(root, "overlays", "client-1.avhdx")
	if err := st.ReleaseAssignment("client-1"); err != nil {
		t.Fatalf("ReleaseAssignment: %v", err)
	}
	if _, ok := st.GetAssignment("client-1"); ok {
		t.Error("released assignment should not be found")
	}
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("overlay should be preserved after release: %v", err)
	}

	// Release unknown client is rejected.
	if err := st.ReleaseAssignment("client-1"); err == nil {
		t.Error("releasing unassigned client should be rejected")
	}
}

func TestStorageListAssignments(t *testing.T) {
	st, root := newTestStorage(t)

	imgPath := makeTestImage(t, root, "base", ".vhdx")
	_, _ = st.RegisterExisting("base-img", "Base Image", imgPath)

	// Empty list.
	if as := st.ListAssignments(); len(as) != 0 {
		t.Errorf("expected 0 assignments, got %d", len(as))
	}

	// Assign multiple clients.
	_, _ = st.AssignClient("client-b", "base-img")
	_, _ = st.AssignClient("client-a", "base-img")
	_, _ = st.AssignClient("client-c", "base-img")

	as := st.ListAssignments()
	if len(as) != 3 {
		t.Errorf("expected 3 assignments, got %d", len(as))
	}
	if as[0].ClientID != "client-a" || as[1].ClientID != "client-b" || as[2].ClientID != "client-c" {
		t.Errorf("assignments not sorted: %+v", as)
	}
}

func TestStorageConcurrentAssign(t *testing.T) {
	st, root := newTestStorage(t)

	imgPath := makeTestImage(t, root, "base", ".vhdx")
	_, _ = st.RegisterExisting("base-img", "Base Image", imgPath)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_, err := st.AssignClient("client-concurrent", "base-img")
			done <- err == nil // only one should succeed
		}(i)
	}
	successes := 0
	for i := 0; i < 10; i++ {
		if <-done {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful assignment under concurrency, got %d", successes)
	}
}

// ---- HTTP endpoint tests ----

func newTestStorageServer(t *testing.T) (*Server, string) {
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
	// Override auto-detected backend with fake for tests.
	fake := newFakeBackend()
	srv.storage.mu.Lock()
	srv.storage.backend = fake
	srv.storage.cap.VHDX = fake.IsAvailable()
	srv.storage.cap.Differencing = fake.IsAvailable()
	srv.storage.mu.Unlock()
	// Register a base image in the storage root.
	imgPath := makeTestImage(t, filepath.Join(dir, "images"), "base", ".vhdx")
	if _, err := srv.storage.RegisterExisting("base-img", "Base Image", imgPath); err != nil {
		t.Fatalf("register image: %v", err)
	}
	return srv, adminTok
}

func TestStorageHealthEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)

	// Unauthorized.
	if rec := do(t, srv, http.MethodGet, "/api/storage/health", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("health without auth = %d, want 401", rec.Code)
	}

	// Authorized.
	rec := do(t, srv, http.MethodGet, "/api/storage/health", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var health StorageHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !health.OK {
		t.Error("health should be OK")
	}
	if health.Images != 1 {
		t.Errorf("health images = %d, want 1", health.Images)
	}
}

func TestStorageHealthMethodNotAllowed(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	if rec := do(t, srv, http.MethodPost, "/api/storage/health", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST health = %d, want 405", rec.Code)
	}
}

func TestStorageImagesListEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)

	// Unauthorized.
	if rec := do(t, srv, http.MethodGet, "/api/storage/images", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("images list without auth = %d, want 401", rec.Code)
	}

	rec := do(t, srv, http.MethodGet, "/api/storage/images", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("images list status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	imgs, _ := body["images"].([]any)
	if len(imgs) != 1 {
		t.Errorf("expected 1 image, got %d", len(imgs))
	}
	if body["count"] != float64(1) {
		t.Errorf("count = %v, want 1", body["count"])
	}
}

func TestStorageImagesGetEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)

	// Existing image.
	rec := do(t, srv, http.MethodGet, "/api/storage/images/base-img", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get image status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	img, _ := body["image"].(map[string]any)
	if img["id"] != "base-img" {
		t.Errorf("image id = %v, want base-img", img["id"])
	}

	// Missing image.
	rec = do(t, srv, http.MethodGet, "/api/storage/images/nope", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("get missing image = %d, want 404", rec.Code)
	}
}

func TestStorageImagesMethodNotAllowed(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	// POST is now valid (create). PUT is not.
	if rec := do(t, srv, http.MethodPut, "/api/storage/images", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT images = %d, want 405", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/storage/images", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE images = %d, want 405", rec.Code)
	}
}

func TestStorageAssignEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	reg := registerClient(t, srv, "PC-ASSIGN", "aa:bb:cc:00:00:50")

	// Unauthorized.
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("assign without auth = %d, want 401", rec.Code)
	}

	// Bad JSON.
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, `{not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("assign bad json = %d, want 400", rec.Code)
	}

	// Missing fields.
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, `{"client_id":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("assign missing image_id = %d, want 400", rec.Code)
	}

	// Successful assign.
	body, _ := json.Marshal(map[string]string{"client_id": reg.ClientID, "image_id": "base-img"})
	rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("assign status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	// Duplicate assign -> conflict.
	rec = do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body))
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate assign = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	// Unknown image.
	body2, _ := json.Marshal(map[string]string{"client_id": reg.ClientID + "-2", "image_id": "nope"})
	rec = do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("assign unknown image = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorageAssignDeleteEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	reg := registerClient(t, srv, "PC-RELEASE", "aa:bb:cc:00:00:51")

	// Assign first.
	body, _ := json.Marshal(map[string]string{"client_id": reg.ClientID, "image_id": "base-img"})
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("pre-assign status = %d, want 201", rec.Code)
	}

	// Unauthorized release.
	if rec := do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("release without auth = %d, want 401", rec.Code)
	}

	// Successful release preserves overlay.
	rec := do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID, adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("release status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var releaseResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &releaseResp); err != nil {
		t.Fatalf("decode release: %v", err)
	}
	if releaseResp["overlay_preserved"] != true {
		t.Errorf("release should preserve overlay: %v", releaseResp)
	}

	// Release again -> not found.
	rec = do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID, adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("double release = %d, want 404", rec.Code)
	}

	// Release unknown client -> not found.
	rec = do(t, srv, http.MethodDelete, "/api/storage/assign/nonexistent", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("release unknown = %d, want 404", rec.Code)
	}
}

func TestStorageAssignDiscardEndpoint(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	reg := registerClient(t, srv, "PC-DISCARD", "aa:bb:cc:00:00:52")

	// Assign first.
	body, _ := json.Marshal(map[string]string{"client_id": reg.ClientID, "image_id": "base-img"})
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("pre-assign status = %d, want 201", rec.Code)
	}

	// Unauthorized discard.
	if rec := do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID+"/overlay", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("discard without auth = %d, want 401", rec.Code)
	}

	// Successful discard deletes overlay.
	rec := do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID+"/overlay", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("discard status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var discardResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &discardResp); err != nil {
		t.Fatalf("decode discard: %v", err)
	}
	if discardResp["discarded"] != true || discardResp["overlay_deleted"] != true {
		t.Errorf("discard should report overlay_deleted: %v", discardResp)
	}

	// Discard again -> not found.
	rec = do(t, srv, http.MethodDelete, "/api/storage/assign/"+reg.ClientID+"/overlay", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("double discard = %d, want 404", rec.Code)
	}

	// Discard unknown client -> not found.
	rec = do(t, srv, http.MethodDelete, "/api/storage/assign/nonexistent/overlay", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("discard unknown = %d, want 404", rec.Code)
	}

	// Assign after discard should work (fresh overlay).
	body2, _ := json.Marshal(map[string]string{"client_id": reg.ClientID, "image_id": "base-img"})
	rec = do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body2))
	if rec.Code != http.StatusCreated {
		t.Errorf("re-assign after discard = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorageAssignMethodNotAllowed(t *testing.T) {
	srv, adminTok := newTestStorageServer(t)
	if rec := do(t, srv, http.MethodGet, "/api/storage/assign", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET assign = %d, want 405", rec.Code)
	}
	if rec := do(t, srv, http.MethodPut, "/api/storage/assign/x", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT assign = %d, want 405", rec.Code)
	}
}
