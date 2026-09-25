package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultServerURL(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("default URL = %q, want %q", cfg.ServerURL, defaultServerURL)
	}
}

func TestNormalizeServerURLValid(t *testing.T) {
	valid := []struct{ in, want string }{
		{"http://127.0.0.1:8888", "http://127.0.0.1:8888"},
		{"http://192.168.1.10:8888", "http://192.168.1.10:8888"},
		{"https://mkyboot.example.com", "https://mkyboot.example.com"},
		{"HTTP://Host.Example:8888", "http://Host.Example:8888"},
		{"  http://127.0.0.1:8888  ", "http://127.0.0.1:8888"},
		{"http://127.0.0.1:8888/", "http://127.0.0.1:8888"},
	}
	for _, tc := range valid {
		got, err := NormalizeServerURL(tc.in)
		if err != nil {
			t.Errorf("NormalizeServerURL(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeServerURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeServerURLRejectsMalformed(t *testing.T) {
	invalid := []string{
		"",
		"not-a-url",
		"127.0.0.1:8888",
		"ftp://example.com",
		"file:///C:/Windows/System32",
		"javascript:alert(1)",
		"http://user:pass@host:8888",
		"https://admin:secret@192.168.0.2:8888",
		"http://",
		"http://127.0.0.1:8888/extra/path",
		"http://127.0.0.1:8888?api=status",
		"http://127.0.0.1:8888#frag",
		"http:///nohost",
		strings.Repeat("a", 3000),
	}
	for _, in := range invalid {
		if got, err := NormalizeServerURL(in); err == nil {
			t.Errorf("NormalizeServerURL(%q) accepted = %q, want rejection", in, got)
		}
	}
}

func TestIsValidServerURL(t *testing.T) {
	if !IsValidServerURL("http://127.0.0.1:8888") {
		t.Error("valid URL rejected")
	}
	if IsValidServerURL("ftp://x") {
		t.Error("invalid URL accepted")
	}
}

func TestConfigSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path, err := configPathIn(dir)
	if err != nil {
		t.Fatalf("configPathIn: %v", err)
	}
	in := Config{ServerURL: "http://192.168.1.10:8888", StartMinimized: true, StartWithWindows: false}
	if err := saveConfigTo(path, &in); err != nil {
		t.Fatalf("saveConfigTo: %v", err)
	}
	out, err := loadConfigFrom(path)
	if err != nil {
		t.Fatalf("loadConfigFrom: %v", err)
	}
	if out.ServerURL != in.ServerURL {
		t.Errorf("roundtrip URL = %q, want %q", out.ServerURL, in.ServerURL)
	}
	if out.StartMinimized != in.StartMinimized {
		t.Errorf("roundtrip StartMinimized = %v", out.StartMinimized)
	}
	if out.StartWithWindows != in.StartWithWindows {
		t.Errorf("roundtrip StartWithWindows = %v", out.StartWithWindows)
	}
}

func TestConfigMissingFileGivesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MKYBOOT", "launcher.json")
	cfg, err := loadConfigFrom(path)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("expected default URL, got %q", cfg.ServerURL)
	}
}

func TestConfigCorruptFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	path, _ := configPathIn(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigFrom(path)
	if err == nil {
		t.Error("corrupt config should return a notice error")
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("corrupt config must fall back to default URL, got %q", cfg.ServerURL)
	}
}

func TestConfigInvalidURLInFileFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path, _ := configPathIn(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"server_url":"ftp://evil"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigFrom(path)
	if err == nil {
		t.Error("invalid persisted URL should return a notice error")
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("invalid persisted URL must fall back to default, got %q", cfg.ServerURL)
	}
}

// TestConfigPersistsNoSecrets guarantees the on-disk format only ever contains
// the whitelisted non-sensitive keys.
func TestConfigPersistsNoSecrets(t *testing.T) {
	cfg := Config{
		ServerURL:        "http://127.0.0.1:8888",
		StartMinimized:   true,
		StartWithWindows: true,
	}
	data, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allowed := map[string]bool{
		"server_url": true, "start_minimized": true, "start_with_windows": true,
	}
	for k := range generic {
		if !allowed[k] {
			t.Errorf("unexpected config key %q (no secrets/credentials allowed)", k)
		}
	}
	lower := strings.ToLower(string(data))
	for _, banned := range []string{"password", "passwd", "secret", "token", "cookie", "api_key", "apikey", "private"} {
		if strings.Contains(lower, banned) {
			t.Errorf("config payload contains banned substring %q: %s", banned, data)
		}
	}
}

func TestSaveRefusesInvalidURL(t *testing.T) {
	cfg := Config{ServerURL: "file:///etc/passwd"}
	if err := cfg.Save(); err == nil {
		t.Error("Save must refuse unsupported scheme")
	}
	cfg = Config{ServerURL: "http://user:pw@host"}
	if err := cfg.Save(); err == nil {
		t.Error("Save must refuse URLs containing credentials")
	}
}

func TestConfigPathLayout(t *testing.T) {
	path, err := configPathIn(`C:\Users\tester\AppData\Roaming`)
	if err != nil {
		t.Fatalf("configPathIn: %v", err)
	}
	want := filepath.Join(`C:\Users\tester\AppData\Roaming`, "MKYBOOT", "launcher.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}
