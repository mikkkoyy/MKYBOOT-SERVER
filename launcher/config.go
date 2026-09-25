package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is the persisted launcher configuration.
//
// It intentionally contains NO credentials, session cookies, tokens or keys.
// The only fields are non-sensitive launcher preferences.
type Config struct {
	ServerURL        string `json:"server_url"`
	StartMinimized   bool   `json:"start_minimized"`
	StartWithWindows bool   `json:"start_with_windows"`
}

const (
	defaultServerURL = "http://127.0.0.1:8888"
	configDirName    = "MKYBOOT"
	configFileName   = "launcher.json"
)

// DefaultConfig returns the safe out-of-box configuration.
func DefaultConfig() Config {
	return Config{ServerURL: defaultServerURL}
}

// NormalizeServerURL validates and normalizes a configured MKYBOOT server URL.
//
// Only http and https schemes are accepted. URLs containing credentials
// (userinfo), query strings, fragments or paths are rejected. A single
// trailing slash is tolerated and stripped so callers can append path/query
// components safely.
func NormalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("server URL is empty")
	}
	if len(raw) > 2048 {
		return "", errors.New("server URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q (only http/https)", u.Scheme)
	}
	if u.User != nil {
		return "", errors.New("server URL must not contain credentials")
	}
	if u.Host == "" {
		return "", errors.New("server URL has no host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("server URL must not contain query or fragment")
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path != "" {
		return "", errors.New("server URL must not contain a path")
	}
	normalized := &url.URL{Scheme: scheme, Host: u.Host}
	return normalized.String(), nil
}

// IsValidServerURL reports whether raw can be normalized successfully.
func IsValidServerURL(raw string) bool {
	_, err := NormalizeServerURL(raw)
	return err == nil
}

// configPath returns the default per-user configuration file location.
//
// On Windows this resolves to %AppData%\MKYBOOT\launcher.json.
func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user config dir: %w", err)
	}
	return configPathIn(base)
}

// configPathIn builds the configuration path under a given base directory.
// It exists so tests can run against a temporary directory.
func configPathIn(base string) (string, error) {
	if base == "" {
		return "", errors.New("config base directory is empty")
	}
	return filepath.Join(base, configDirName, configFileName), nil
}

// LoadConfig reads the configuration. It never fails hard: missing, unreadable
// or corrupt configuration files fall back to safe defaults together with a
// non-nil error the caller may surface as a notice.
func LoadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return DefaultConfig(), err
	}
	return loadConfigFrom(path)
}

func loadConfigFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), fmt.Errorf("failed to read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("failed to parse config, using defaults: %w", err)
	}
	// Independently validate/normalize each persisted field. Invalid values
	// fall back to defaults instead of propagating.
	if cfg.ServerURL == "" {
		cfg.ServerURL = defaultServerURL
	} else if norm, err := NormalizeServerURL(cfg.ServerURL); err == nil {
		cfg.ServerURL = norm
	} else {
		cfg = DefaultConfig()
		return cfg, fmt.Errorf("config contained invalid server URL, using default: %w", err)
	}
	return cfg, nil
}

// Save validates then persists the configuration atomically.
// Only the whitelisted Config fields are ever written.
func (c *Config) Save() error {
	if c == nil {
		return errors.New("nil config")
	}
	norm, err := NormalizeServerURL(c.ServerURL)
	if err != nil {
		return fmt.Errorf("refusing to save invalid server URL: %w", err)
	}
	c.ServerURL = norm
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveConfigTo(path, c)
}

func saveConfigTo(path string, c *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to replace config: %w", err)
	}
	return nil
}

// setStartWithWindows toggles the per-user "start with Windows" registry
// entry (HKCU\...\Run). It never requires Administrator privileges.
func setStartWithWindows(enabled bool, exePath string) error {
	const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	const valueName = "MKYBOOT Launcher"

	if enabled {
		if exePath == "" {
			return errors.New("cannot enable start with Windows: executable path unknown")
		}
		return regSetStringValue(runKey, valueName, `"`+exePath+`"`)
	}
	return regDeleteStringValue(runKey, valueName)
}
