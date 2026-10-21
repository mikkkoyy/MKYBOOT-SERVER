package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.BindAddress != "127.0.0.1" {
		t.Errorf("default bind = %q, want 127.0.0.1", c.BindAddress)
	}
	if c.BindPort != 8888 {
		t.Errorf("default port = %d, want 8888", c.BindPort)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		in   Config
		ok   bool
	}{
		{"empty defaults to localhost", Config{}, true},
		{"valid ip", Config{BindAddress: "127.0.0.1", BindPort: 9000}, true},
		{"valid host localhost", Config{BindAddress: "localhost", BindPort: 80}, true},
		{"port too low", Config{BindAddress: "127.0.0.1", BindPort: 0}, true}, // 0 -> default
		{"port out of range high", Config{BindAddress: "127.0.0.1", BindPort: 70000}, false},
		{"port negative", Config{BindAddress: "127.0.0.1", BindPort: -1}, false},
		{"bad host", Config{BindAddress: "not a host!!", BindPort: 8888}, false},
		{"address too long", Config{BindAddress: string(make([]byte, 300)), BindPort: 8888}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.in
			err := c.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() error = %v, want ok", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
		})
	}
}

func TestConfigSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	in := Config{BindAddress: "127.0.0.1", BindPort: 9443}
	if err := in.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.BindAddress != in.BindAddress || out.BindPort != in.BindPort {
		t.Errorf("roundtrip = %+v, want %+v", out, in)
	}
}

func TestConfigMissingFileGivesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if c.BindAddress != "127.0.0.1" || c.BindPort != 8888 {
		t.Errorf("expected defaults, got %+v", c)
	}
}

func TestConfigCorruptFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err == nil {
		t.Error("corrupt config should return a notice error")
	}
	if c.BindAddress != "127.0.0.1" {
		t.Errorf("corrupt config must fall back to defaults, got %+v", c)
	}
}

func TestConfigInvalidValueFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"bind_port": 99999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Error("invalid config should return a notice error")
	}
}

func TestConfigLogFilePath(t *testing.T) {
	t.Setenv("MKYBOOT_DATA_DIR", filepath.Join(t.TempDir(), "data"))

	c := DefaultConfig()
	if got, want := c.LogFilePath(), DefaultLogPath(); got != want {
		t.Errorf("LogFilePath() = %q, want %q", got, want)
	}

	c.LogDir = "D:/MKYBOOT-Lab/Logs"
	want := filepath.Join("D:/MKYBOOT-Lab/Logs", "mkyboot-server.log")
	if got := c.LogFilePath(); got != want {
		t.Errorf("LogFilePath() = %q, want %q", got, want)
	}
}

func TestConfigPersistsNoSecrets(t *testing.T) {
	c := DefaultConfig()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"token", "password", "secret", "cookie", "private"} {
		if contains(string(data), banned) {
			t.Errorf("config file contains banned substring %q: %s", banned, data)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
