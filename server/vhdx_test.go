package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---- Fake VHDX backend for tests ----

// FakeVHDXBackend implements VHDXBackend for testing. It tracks created,
// attached, and removed files in memory and creates real zero-length files
// so os.Stat works during tests.
type FakeVHDXBackend struct {
	mu        sync.Mutex
	files     map[string]int64  // path -> size bytes
	attached  map[string]bool   // path -> attached
	parents   map[string]string // differencing child path -> parent path
	failOp    string            // operation name to fail
	failErr   error             // error to return for the failing op
	createOps []string          // recorded create operations
}

func newFakeBackend() *FakeVHDXBackend {
	return &FakeVHDXBackend{
		files:    make(map[string]int64),
		attached: make(map[string]bool),
		parents:  make(map[string]string),
	}
}

func (b *FakeVHDXBackend) Name() string { return "fake" }

func (b *FakeVHDXBackend) IsAvailable() bool { return true }

// fail sets an operation to return failErr. Empty string clears the failure.
func (b *FakeVHDXBackend) fail(op string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failOp = op
	b.failErr = err
}

func (b *FakeVHDXBackend) checkFail(op string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failOp == op {
		return b.failErr
	}
	return nil
}

func (b *FakeVHDXBackend) ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

func (b *FakeVHDXBackend) CreateFixed(path string, sizeMB int64) error {
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
	b.files[path] = sizeMB * 1024 * 1024
	b.createOps = append(b.createOps, fmt.Sprintf("fixed:%s", path))
	b.mu.Unlock()
	return nil
}

func (b *FakeVHDXBackend) CreateExpandable(path string, sizeMB int64) error {
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
	b.files[path] = sizeMB * 1024 * 1024
	b.createOps = append(b.createOps, fmt.Sprintf("expandable:%s", path))
	b.mu.Unlock()
	return nil
}

func (b *FakeVHDXBackend) CreateDifferencing(path string, parentPath string) error {
	if err := b.checkFail("create"); err != nil {
		return err
	}
	if _, err := os.Stat(parentPath); err != nil {
		return fmt.Errorf("parent vhdx not found: %s", parentPath)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureDir(path); err != nil {
		return err
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return err
	}
	b.files[path] = 0
	b.parents[path] = parentPath
	b.createOps = append(b.createOps, fmt.Sprintf("differencing:%s parent=%s", path, parentPath))
	return nil
}

func (b *FakeVHDXBackend) Attach(path string, readOnly bool) error {
	if err := b.checkFail("attach"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.files[path]; !exists {
		return fmt.Errorf("vhdx not found: %s", path)
	}
	b.attached[path] = true
	return nil
}

func (b *FakeVHDXBackend) Detach(path string) error {
	if err := b.checkFail("detach"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.attached, path)
	return nil
}

func (b *FakeVHDXBackend) Remove(path string) error {
	if err := b.checkFail("remove"); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.files, path)
	delete(b.attached, path)
	delete(b.parents, path)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove vhdx %q: %w", path, err)
	}
	return nil
}

func (b *FakeVHDXBackend) ListAttached() ([]string, error) {
	if err := b.checkFail("list"); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for p, attached := range b.attached {
		if attached {
			out = append(out, p)
		}
	}
	return out, nil
}

func (b *FakeVHDXBackend) InspectAttached(path string) (*VHDXInfo, error) {
	if err := b.checkFail("inspect"); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.attached[path] {
		return nil, fmt.Errorf("vhdx not attached: %s", path)
	}
	info := &VHDXInfo{
		Filename: path,
		Attached: true,
	}
	if parent, isChild := b.parents[path]; isChild {
		info.IsChild = true
		info.ParentFile = parent
	}
	return info, nil
}

func (b *FakeVHDXBackend) fileSize(path string) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.files[path]
	return v, ok
}

func (b *FakeVHDXBackend) isAttached(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attached[path]
}

func (b *FakeVHDXBackend) parentOf(path string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	parent, ok := b.parents[path]
	return parent, ok
}

// ---- Test helpers ----

func newTestStorageWithBackend(t *testing.T, backend VHDXBackend) (*Storage, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "images")
	st, err := NewStorage(root, NewLogger(io.Discard))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if backend != nil {
		// Replace the auto-detected backend with the test backend.
		st.mu.Lock()
		st.backend = backend
		st.cap.VHDX = backend.IsAvailable()
		st.cap.Differencing = backend.IsAvailable()
		st.mu.Unlock()
	}
	return st, root
}

func makeTestVHDXFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".vhdx")
	if err := os.WriteFile(path, []byte("vhdx-test-data"), 0o600); err != nil {
		t.Fatalf("write test vhdx: %v", err)
	}
	return path
}

// ---- Tests ----

func TestStorageBackendDetected(t *testing.T) {
	st, _ := newTestStorageWithBackend(t, newFakeBackend())
	if !st.BackendAvailable() {
		t.Error("backend should be available")
	}
	if got := st.BackendName(); got != "fake" {
		t.Errorf("backend name = %q, want %q", got, "fake")
	}
	cap := st.Capabilities()
	if !cap.VHDX {
		t.Error("VHDX capability should be true with fake backend")
	}
	if !cap.Differencing {
		t.Error("Differencing capability should be true with fake backend")
	}
	if cap.BlockIsolation {
		t.Error("BlockIsolation should be false")
	}
	if cap.ISCSI {
		t.Error("ISCSI should be false")
	}
}

func TestStorageNoBackend(t *testing.T) {
	st, _ := newTestStorageWithBackend(t, newFakeBackend())
	// Simulate no backend by niling it out.
	st.mu.Lock()
	st.backend = nil
	st.cap.VHDX = false
	st.cap.Differencing = false
	st.mu.Unlock()

	if st.BackendAvailable() {
		t.Error("backend should not be available when nil")
	}
	if got := st.BackendName(); got != "none" {
		t.Errorf("backend name = %q, want %q", got, "none")
	}
	cap := st.Capabilities()
	if cap.VHDX || cap.Differencing {
		t.Error("capabilities should be false without backend")
	}
}

func TestStorageCreateImage(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "win10-base.vhdx")
	img, err := st.CreateImage("win10", "Windows 10 Base", imgPath, ImageFormatVHDX, 40960)
	if err != nil {
		t.Fatalf("CreateImage: %v", err)
	}
	if img.ID != "win10" {
		t.Errorf("id = %q, want %q", img.ID, "win10")
	}
	if img.Format != ImageFormatVHDX {
		t.Errorf("format = %v, want VHDX", img.Format)
	}
	if !img.ReadOnly {
		t.Error("read_only should be true")
	}
	if img.Status != ImageStatusReady {
		t.Errorf("status = %v, want Ready", img.Status)
	}
	if _, ok := backend.fileSize(imgPath); !ok {
		t.Error("backend should have created the vhdx file")
	}

	// Verify the image is registered.
	got, ok := st.GetImage("win10")
	if !ok || got.ID != "win10" {
		t.Error("image not registered after creation")
	}
}

func TestStorageCreateImageValidation(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	// Empty id.
	if _, err := st.CreateImage("", "name", filepath.Join(root, "x.vhdx"), ImageFormatVHDX, 1024); err == nil {
		t.Error("empty id should be rejected")
	}

	// Invalid id chars.
	if _, err := st.CreateImage("bad id!", "name", filepath.Join(root, "x.vhdx"), ImageFormatVHDX, 1024); err == nil {
		t.Error("invalid id chars should be rejected")
	}

	// Empty name.
	if _, err := st.CreateImage("img1", "", filepath.Join(root, "x.vhdx"), ImageFormatVHDX, 1024); err == nil {
		t.Error("empty name should be rejected")
	}

	// Path outside root.
	outside := filepath.Join(filepath.Dir(root), "outside.vhdx")
	if _, err := st.CreateImage("img2", "name", outside, ImageFormatVHDX, 1024); err == nil {
		t.Error("path outside root should be rejected")
	}

	// Wrong extension for format.
	if _, err := st.CreateImage("img3", "name", filepath.Join(root, "x.vhd"), ImageFormatVHDX, 1024); err == nil {
		t.Error("wrong extension should be rejected")
	}

	// Size too small.
	if _, err := st.CreateImage("img4", "name", filepath.Join(root, "x.vhdx"), ImageFormatVHDX, 0); err == nil {
		t.Error("zero size should be rejected")
	}

	// Size too large.
	if _, err := st.CreateImage("img5", "name", filepath.Join(root, "x.vhdx"), ImageFormatVHDX, maxImageSizeMB+1); err == nil {
		t.Error("oversized should be rejected")
	}

	// Duplicate id.
	_, _ = st.CreateImage("img6", "name", filepath.Join(root, "img6.vhdx"), ImageFormatVHDX, 1024)
	if _, err := st.CreateImage("img6", "dup", filepath.Join(root, "img6b.vhdx"), ImageFormatVHDX, 1024); err == nil {
		t.Error("duplicate id should be rejected")
	}

	// File already exists.
	existing := makeTestVHDXFile(t, root, "already")
	if _, err := st.CreateImage("img7", "name", existing, ImageFormatVHDX, 1024); err == nil {
		t.Error("existing file should be rejected")
	}
}

func TestStorageCreateImageBackendUnavailable(t *testing.T) {
	st, root := newTestStorageWithBackend(t, newFakeBackend())
	// Simulate no backend.
	st.mu.Lock()
	st.backend = nil
	st.cap.VHDX = false
	st.cap.Differencing = false
	st.mu.Unlock()

	_, err := st.CreateImage("img", "name", filepath.Join(root, "img.vhdx"), ImageFormatVHDX, 1024)
	if err == nil {
		t.Error("create without backend should fail")
	}
	if !strings.Contains(err.Error(), "backend") {
		t.Errorf("error should mention backend: %v", err)
	}
}

func TestStorageCreateImageBackendFailure(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)
	backend.fail("create", fmt.Errorf("diskpart error"))
	_, err := st.CreateImage("img", "name", filepath.Join(root, "img.vhdx"), ImageFormatVHDX, 1024)
	if err == nil {
		t.Error("backend create failure should propagate")
	}
	// Clear failure and verify it works.
	backend.fail("", nil)
	_, err = st.CreateImage("img", "name", filepath.Join(root, "img.vhdx"), ImageFormatVHDX, 1024)
	if err != nil {
		t.Errorf("create after clearing failure: %v", err)
	}
}

func TestStorageCreateDifferencingImage(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	parent := filepath.Join(root, "parent.vhdx")
	_, _ = st.CreateImage("parent", "Parent", parent, ImageFormatVHDX, 1024)

	child := filepath.Join(root, "child.vhdx")
	img, err := st.CreateImage("child", "Child Diff", child, ImageFormatVHDX, 0, parent)
	if err != nil {
		t.Fatalf("CreateImage differencing: %v", err)
	}
	// Verify parent-child relationship was recorded by the fake backend.
	parentOf, ok := backend.parentOf(child)
	if !ok {
		t.Error("backend should record parent-child relationship")
	}
	if parentOf != parent {
		t.Errorf("parent = %q, want %q", parentOf, parent)
	}
	_ = img
}

func TestStorageRemoveImage(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "removable.vhdx")
	_, _ = st.CreateImage("removable", "Removable", imgPath, ImageFormatVHDX, 1024)

	if err := st.RemoveImage("removable"); err != nil {
		t.Fatalf("RemoveImage: %v", err)
	}
	if _, ok := st.GetImage("removable"); ok {
		t.Error("image should be removed")
	}
	if _, ok := backend.fileSize(imgPath); ok {
		t.Error("backend file should be removed")
	}
}

func TestStorageRemoveImageNotFound(t *testing.T) {
	st, _ := newTestStorageWithBackend(t, newFakeBackend())
	if err := st.RemoveImage("nope"); err == nil {
		t.Error("removing unknown image should fail")
	}
}

func TestStorageRemoveImageInUse(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "inuse.vhdx")
	_, _ = st.CreateImage("inuse", "In Use", imgPath, ImageFormatVHDX, 1024)
	// Assign a client to the image.
	if _, err := st.AssignClient("client-1", "inuse"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := st.RemoveImage("inuse"); err == nil {
		t.Error("removing in-use image should fail")
	}
	// Verify image still exists.
	if _, ok := st.GetImage("inuse"); !ok {
		t.Error("image should still exist after failed removal")
	}
}

func TestStorageRemoveImageAttached(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "attached.vhdx")
	_, _ = st.CreateImage("attached", "Attached", imgPath, ImageFormatVHDX, 1024)
	// Attach the image via backend.
	if err := backend.Attach(imgPath, false); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := st.RemoveImage("attached"); err == nil {
		t.Error("removing attached image should fail")
	}
}

func TestStorageInspectImage(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "inspect.vhdx")
	_, _ = st.CreateImage("inspect", "Inspect Me", imgPath, ImageFormatVHDX, 1024)

	img, vinfo, err := st.InspectImage("inspect")
	if err != nil {
		t.Fatalf("InspectImage: %v", err)
	}
	if img.ID != "inspect" {
		t.Errorf("image id = %q, want %q", img.ID, "inspect")
	}
	if vinfo.Attached {
		t.Error("unattached image should report attached=false")
	}
	if vinfo.Filename != imgPath {
		t.Errorf("filename = %q, want %q", vinfo.Filename, imgPath)
	}
}

func TestStorageInspectImageDifferencingParent(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	parent := filepath.Join(root, "parent.vhdx")
	_, _ = st.CreateImage("parent", "Parent", parent, ImageFormatVHDX, 1024)

	child := filepath.Join(root, "child.vhdx")
	_, _ = st.CreateImage("child", "Child", child, ImageFormatVHDX, 0, parent)
	// Attach child so inspect can read parent info.
	if err := backend.Attach(child, false); err != nil {
		t.Fatalf("attach: %v", err)
	}

	_, vinfo, err := st.InspectImage("child")
	if err != nil {
		t.Fatalf("InspectImage child: %v", err)
	}
	if !vinfo.IsChild {
		t.Error("child should report IsChild=true")
	}
	if vinfo.ParentFile != parent {
		t.Errorf("parent file = %q, want %q", vinfo.ParentFile, parent)
	}
}

func TestStorageInspectImageNotFound(t *testing.T) {
	st, _ := newTestStorageWithBackend(t, newFakeBackend())
	_, _, err := st.InspectImage("nope")
	if err == nil {
		t.Error("inspecting unknown image should fail")
	}
}

func TestStorageImageIDValidation(t *testing.T) {
	valid := []string{"win10", "image-1", "img_2", "A", "a1-b2_c3"}
	for _, id := range valid {
		if err := validateImageID(id); err != nil {
			t.Errorf("validateImageID(%q) = %v, want ok", id, err)
		}
	}
	invalid := []string{"", "bad id", "bad/id", "bad.id", "-leading", ".hidden", "id/../escape"}
	for _, id := range invalid {
		if err := validateImageID(id); err == nil {
			t.Errorf("validateImageID(%q) = nil, want error", id)
		}
	}
}

func TestStorageConcurrentCreateAndRemove(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	done := make(chan error, 20)
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := fmt.Sprintf("img-%d", n)
			path := filepath.Join(root, id+".vhdx")
			_, err := st.CreateImage(id, fmt.Sprintf("Image %d", n), path, ImageFormatVHDX, 1024)
			done <- err
		}(i)
	}
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent create: %v", err)
		}
	}
	// All 10 should exist.
	imgs := st.ListImages()
	if len(imgs) != 10 {
		t.Errorf("expected 10 images, got %d", len(imgs))
	}
}

// ---- HTTP endpoint tests ----

func newTestStorageVHDXServer(t *testing.T, backend VHDXBackend) (*Server, string, string) {
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
	// Replace backend with test backend.
	if backend != nil {
		srv.storage.mu.Lock()
		srv.storage.backend = backend
		srv.storage.cap.VHDX = backend.IsAvailable()
		srv.storage.cap.Differencing = backend.IsAvailable()
		srv.storage.mu.Unlock()
	}
	// Register a base image.
	imgPath := filepath.Join(dir, "images", "base.vhdx")
	os.MkdirAll(filepath.Dir(imgPath), 0o700)
	if _, err := srv.storage.CreateImage("base-img", "Base Image", imgPath, ImageFormatVHDX, 1024); err != nil {
		t.Fatalf("register image: %v", err)
	}
	return srv, adminTok, dir
}

func TestStorageHealthEndpointVHDX(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	rec := do(t, srv, http.MethodGet, "/api/storage/health", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var health StorageHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !health.OK {
		t.Error("health should be OK")
	}
	if !strings.Contains(health.Message, "online") {
		t.Errorf("health message should mention online: %s", health.Message)
	}
}

func TestStorageCreateImageEndpoint(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, dir := newTestStorageVHDXServer(t, backend)

	body, _ := json.Marshal(map[string]any{
		"id":      "new-img",
		"name":    "New Image",
		"path":    filepath.Join(dir, "images", "new-img.vhdx"),
		"format":  "vhdx",
		"size_mb": 2048,
	})
	rec := do(t, srv, http.MethodPost, "/api/storage/images", adminTok, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	// Duplicate -> conflict.
	rec = do(t, srv, http.MethodPost, "/api/storage/images", adminTok, string(body))
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate create = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	// Unauthorized.
	rec = do(t, srv, http.MethodPost, "/api/storage/images", "", string(body))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("create without auth = %d, want 401", rec.Code)
	}

	// Bad format.
	badBody, _ := json.Marshal(map[string]any{
		"id":      "bad-fmt",
		"name":    "Bad",
		"path":    filepath.Join(dir, "images", "bad.vhdx"),
		"format":  "iso",
		"size_mb": 1024,
	})
	rec = do(t, srv, http.MethodPost, "/api/storage/images", adminTok, string(badBody))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad format = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorageCreateImageEndpointValidation(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, dir := newTestStorageVHDXServer(t, backend)

	// Path outside root.
	outside := filepath.Join(filepath.Dir(dir), "outside.vhdx")
	body, _ := json.Marshal(map[string]any{
		"id":      "outside",
		"name":    "Outside",
		"path":    outside,
		"format":  "vhdx",
		"size_mb": 1024,
	})
	rec := do(t, srv, http.MethodPost, "/api/storage/images", adminTok, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("outside path = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	// Missing fields.
	body, _ = json.Marshal(map[string]any{"id": "no-name"})
	rec = do(t, srv, http.MethodPost, "/api/storage/images", adminTok, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing fields = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	// Malformed JSON.
	rec = do(t, srv, http.MethodPost, "/api/storage/images", adminTok, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed json = %d, want 400", rec.Code)
	}
}

func TestStorageRemoveImageEndpoint(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, dir := newTestStorageVHDXServer(t, backend)

	// Create an image to remove.
	imgPath := filepath.Join(dir, "images", "remove-me.vhdx")
	if _, err := srv.storage.CreateImage("remove-me", "Remove Me", imgPath, ImageFormatVHDX, 1024); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Remove it.
	rec := do(t, srv, http.MethodDelete, "/api/storage/images/remove-me", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	// Remove again -> not found.
	rec = do(t, srv, http.MethodDelete, "/api/storage/images/remove-me", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("double remove = %d, want 404", rec.Code)
	}

	// Unauthorized.
	rec = do(t, srv, http.MethodDelete, "/api/storage/images/base-img", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("remove without auth = %d, want 401", rec.Code)
	}
}

func TestStorageRemoveImageInUseEndpoint(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	// Register a client and assign them to base-img.
	reg := registerClient(t, srv, "PC-INUSE", "aa:bb:cc:00:00:60")
	body, _ := json.Marshal(map[string]string{"client_id": reg.ClientID, "image_id": "base-img"})
	if rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("pre-assign: %d body=%s", rec.Code, rec.Body.String())
	}

	// Try to remove in-use image -> conflict.
	rec := do(t, srv, http.MethodDelete, "/api/storage/images/base-img", adminTok, "")
	if rec.Code != http.StatusConflict {
		t.Errorf("remove in-use = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorageInspectImageEndpoint(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	rec := do(t, srv, http.MethodGet, "/api/storage/images/base-img/inspect", adminTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	img, _ := body["image"].(map[string]any)
	if img["id"] != "base-img" {
		t.Errorf("image id = %v, want base-img", img["id"])
	}
	inspect, _ := body["inspect"].(map[string]any)
	if inspect["attached"] != false {
		t.Errorf("attached should be false, got %v", inspect["attached"])
	}
}

func TestStorageInspectImageNotFoundEndpoint(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	rec := do(t, srv, http.MethodGet, "/api/storage/images/nope/inspect", adminTok, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("inspect missing = %d, want 404", rec.Code)
	}
}

func TestStorageMethodEnforcement(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	// GET on create-only endpoint.
	if rec := do(t, srv, http.MethodGet, "/api/storage/images", adminTok, ""); rec.Code != http.StatusOK {
		t.Errorf("GET list = %d, want 200", rec.Code)
	}
	// DELETE on list endpoint.
	if rec := do(t, srv, http.MethodDelete, "/api/storage/images", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE list = %d, want 405", rec.Code)
	}
	// PUT on image endpoint.
	if rec := do(t, srv, http.MethodPut, "/api/storage/images/base-img", adminTok, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT image = %d, want 405", rec.Code)
	}
}

func TestStorageConcurrentAssignVHDX(t *testing.T) {
	backend := newFakeBackend()
	srv, adminTok, _ := newTestStorageVHDXServer(t, backend)

	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			body, _ := json.Marshal(map[string]string{"client_id": "client-race", "image_id": "base-img"})
			rec := do(t, srv, http.MethodPost, "/api/storage/assign", adminTok, string(body))
			done <- rec.Code
		}()
	}
	successes := 0
	conflicts := 0
	for i := 0; i < 10; i++ {
		code := <-done
		if code == http.StatusCreated {
			successes++
		} else if code == http.StatusConflict {
			conflicts++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful assign under concurrency, got %d successes, %d conflicts", successes, conflicts)
	}
}

func TestStorageReleasePreservesOverlay(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "base.vhdx")
	_, _ = st.CreateImage("base-img", "Base Image", imgPath, ImageFormatVHDX, 1024)

	// Assign client -> overlay file is created.
	if _, err := st.AssignClient("client-1", "base-img"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	overlayPath := filepath.Join(root, "overlays", "client-1.avhdx")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Fatalf("overlay should exist after assign: %v", err)
	}

	// Release -> overlay file is preserved.
	if err := st.ReleaseAssignment("client-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok := st.GetAssignment("client-1"); ok {
		t.Error("assignment should be cleared after release")
	}
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("overlay should be preserved after release: %v", err)
	}
}

func TestStorageDiscardDeletesOverlay(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "base.vhdx")
	_, _ = st.CreateImage("base-img", "Base Image", imgPath, ImageFormatVHDX, 1024)

	// Assign client -> overlay file is created.
	if _, err := st.AssignClient("client-1", "base-img"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	overlayPath := filepath.Join(root, "overlays", "client-1.avhdx")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Fatalf("overlay should exist after assign: %v", err)
	}

	// Discard -> overlay file is deleted.
	if err := st.DiscardAssignment("client-1"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, ok := st.GetAssignment("client-1"); ok {
		t.Error("assignment should be cleared after discard")
	}
	if _, err := os.Stat(overlayPath); err == nil {
		t.Error("overlay should be deleted after discard")
	}
}

func TestStorageDiscardNotFound(t *testing.T) {
	st, _ := newTestStorageWithBackend(t, newFakeBackend())
	if err := st.DiscardAssignment("nope"); err == nil {
		t.Error("discarding unknown client should fail")
	}
}

func TestStorageDiscardThenReassign(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "base.vhdx")
	_, _ = st.CreateImage("base-img", "Base Image", imgPath, ImageFormatVHDX, 1024)

	// Assign, discard, re-assign -> should work.
	if _, err := st.AssignClient("client-1", "base-img"); err != nil {
		t.Fatalf("first assign: %v", err)
	}
	if err := st.DiscardAssignment("client-1"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := st.AssignClient("client-1", "base-img"); err != nil {
		t.Fatalf("re-assign after discard: %v", err)
	}
}

func TestStorageDiscardAttachedOverlay(t *testing.T) {
	backend := newFakeBackend()
	st, root := newTestStorageWithBackend(t, backend)

	imgPath := filepath.Join(root, "base.vhdx")
	_, _ = st.CreateImage("base-img", "Base Image", imgPath, ImageFormatVHDX, 1024)

	// Assign client -> overlay file created.
	if _, err := st.AssignClient("client-1", "base-img"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	overlayPath := filepath.Join(root, "overlays", "client-1.avhdx")

	// Attach overlay via backend.
	if err := backend.Attach(overlayPath, false); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// Discard -> detaches then deletes overlay.
	if err := st.DiscardAssignment("client-1"); err != nil {
		t.Fatalf("discard attached: %v", err)
	}
	if backend.isAttached(overlayPath) {
		t.Error("overlay should be detached after discard")
	}
	if _, err := os.Stat(overlayPath); err == nil {
		t.Error("overlay should be deleted after discard")
	}
}
