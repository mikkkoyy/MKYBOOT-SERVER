package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// adminTokenEnvVar lets an operator provision the initial administrator token
// non-interactively (e.g. during automated install). It is read once at first
// run; the plaintext is never persisted.
const adminTokenEnvVar = "MKYBOOT_ADMIN_TOKEN"

// adminRecord is the persisted administrator credential. It stores only a
// SHA-256 hash of the token, never the token itself.
type adminRecord struct {
	TokenHash string `json:"token_sha256"`
	// Provisioned records how the token was first set, for operator clarity.
	Provisioned string `json:"provisioned"` // "env" | "generated"
}

// hashToken returns the hex SHA-256 of a token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// adminStore provisions and verifies the administrator token. The token file
// lives under the data root with 0600 permissions.
type adminStore struct {
	path string
}

func newAdminStore(path string) *adminStore { return &adminStore{path: path} }

// load reads the stored hash. A missing file returns a zero record and no
// error; a corrupt file returns an actionable error.
func (a *adminStore) load() (adminRecord, error) {
	var rec adminRecord
	data, err := os.ReadFile(a.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rec, nil
		}
		return rec, fmt.Errorf("read admin token file: %w", err)
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("parse admin token file: %w", err)
	}
	return rec, nil
}

// IsProvisioned reports whether an administrator token has been set.
func (a *adminStore) IsProvisioned() bool {
	rec, err := a.load()
	return err == nil && rec.TokenHash != ""
}

// Provision sets the administrator token on first run.
//
// Order of preference:
//  1. If already provisioned, this is a no-op (returns exists=true).
//  2. Otherwise, if MKYBOOT_ADMIN_TOKEN is set, use it (provisioned="env").
//  3. Otherwise generate a random 256-bit token (provisioned="generated") and
//     return it so the caller can display it exactly once.
//
// Only the SHA-256 hash is written to disk. The plaintext is returned to the
// caller for one-time display and never logged or persisted.
func (a *adminStore) Provision() (plaintext string, generated bool, exists bool, err error) {
	rec, err := a.load()
	if err != nil {
		return "", false, false, err
	}
	if rec.TokenHash != "" {
		return "", false, true, nil
	}
	if envTok := strings.TrimSpace(os.Getenv(adminTokenEnvVar)); envTok != "" {
		if len(envTok) < 16 {
			return "", false, false, fmt.Errorf("%s must be at least 16 characters", adminTokenEnvVar)
		}
		rec = adminRecord{TokenHash: hashToken(envTok), Provisioned: "env"}
		if err := a.save(rec); err != nil {
			return "", false, false, err
		}
		return "", false, false, nil
	}
	tok, err := generateToken(32)
	if err != nil {
		return "", false, false, err
	}
	rec = adminRecord{TokenHash: hashToken(tok), Provisioned: "generated"}
	if err := a.save(rec); err != nil {
		return "", false, false, err
	}
	return tok, true, false, nil
}

func (a *adminStore) save(rec adminRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(a.path, append(data, '\n'), 0o600)
}

// Verify reports whether the supplied token matches the stored hash, using a
// constant-time comparison. An unprovisioned store verifies nothing.
func (a *adminStore) Verify(token string) bool {
	if token == "" {
		return false
	}
	rec, err := a.load()
	if err != nil || rec.TokenHash == "" {
		return false
	}
	return constantTimeEqual(hashToken(token), rec.TokenHash)
}
