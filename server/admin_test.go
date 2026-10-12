package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminProvisionGeneratedOnce(t *testing.T) {
	dir := t.TempDir()
	// Hermetic: ensure no ambient admin token influences provisioning.
	t.Setenv(adminTokenEnvVar, "")
	a := newAdminStore(filepath.Join(dir, "admin.token.json"))

	if a.IsProvisioned() {
		t.Fatal("should not be provisioned initially")
	}
	tok, generated, exists, err := a.Provision()
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !generated || exists || tok == "" {
		t.Fatalf("expected generated token, got generated=%v exists=%v tok=%q", generated, exists, tok)
	}
	if !a.IsProvisioned() {
		t.Fatal("should be provisioned after Provision")
	}
	// Second call must be a no-op and not return the token again.
	tok2, generated2, exists2, err := a.Provision()
	if err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if generated2 || !exists2 || tok2 != "" {
		t.Fatalf("second provision should be no-op, got generated=%v exists=%v tok=%q", generated2, exists2, tok2)
	}
}

func TestAdminVerify(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(adminTokenEnvVar, "supersecrettoken-value")
	a := newAdminStore(filepath.Join(dir, "admin.token.json"))
	if _, _, _, err := a.Provision(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !a.Verify("supersecrettoken-value") {
		t.Error("correct token must verify")
	}
	if a.Verify("wrong") {
		t.Error("wrong token must not verify")
	}
	if a.Verify("") {
		t.Error("empty token must not verify")
	}
}

func TestAdminEnvTokenTooShort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(adminTokenEnvVar, "short")
	a := newAdminStore(filepath.Join(dir, "admin.token.json"))
	if _, _, _, err := a.Provision(); err == nil {
		t.Error("short env token must be rejected")
	}
}

// TestAdminTokenNotStoredPlaintext guarantees the on-disk file never contains
// the plaintext token.
func TestAdminTokenNotStoredPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.token.json")
	t.Setenv(adminTokenEnvVar, "plaintext-should-not-appear-1234")
	a := newAdminStore(path)
	if _, _, _, err := a.Provision(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plaintext-should-not-appear-1234") {
		t.Error("admin token file contains the plaintext token")
	}
}
