package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSecureArchivePathRejectsTraversal(t *testing.T) {
	dest := t.TempDir()

	if _, err := secureArchivePath(dest, "../evil.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	if _, err := secureArchivePath(dest, "/tmp/evil.txt"); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}

func TestSecureArchivePathAllowsNestedContent(t *testing.T) {
	dest := t.TempDir()

	target, err := secureArchivePath(dest, "ha-nova/bundle.json")
	if err != nil {
		t.Fatalf("secureArchivePath() error: %v", err)
	}
	if target != filepath.Join(dest, "ha-nova", "bundle.json") {
		t.Fatalf("unexpected target path: %s", target)
	}
}

func TestValidateBundleRootRejectsMissingBundleMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if err := validateBundleRoot(root); err == nil {
		t.Fatal("expected missing bundle.json to fail validation")
	}
}

func TestValidateBundleRootAcceptsBundle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bundle.json"), []byte(fmt.Sprintf(`{"bundle_format_version":1,"os":"%s","arch":"%s","binary_name":"%s"}`, bundlePlatformOS(), bundlePlatformArch(), publicBinaryName())), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if err := validateBundleRoot(root); err != nil {
		t.Fatalf("validateBundleRoot() error: %v", err)
	}
}

func TestValidateBundleRootRejectsMismatchedBundleMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bundle.json"), []byte(`{"bundle_format_version":1,"os":"windows","arch":"amd64","binary_name":"relay.exe"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if err := validateBundleRoot(root); err == nil {
		t.Fatal("expected mismatched bundle metadata to fail validation")
	}
}

func TestVerifyFileChecksumAcceptsMatchingSHA256(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "ha-nova-linux-amd64.tar.gz")
	payload := []byte("bundle")
	if err := os.WriteFile(archivePath, payload, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	sum := sha256.Sum256(payload)
	manifest := fmt.Sprintf("%x  %s", sum, filepath.Base(archivePath))

	if err := verifyFileChecksum(archivePath, manifest); err != nil {
		t.Fatalf("verifyFileChecksum() error: %v", err)
	}
}

func TestVerifyFileChecksumRejectsMismatchedSHA256(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "ha-nova-linux-amd64.tar.gz")
	if err := os.WriteFile(archivePath, []byte("bundle"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	if err := verifyFileChecksum(archivePath, "deadbeef  ha-nova-linux-amd64.tar.gz"); err == nil {
		t.Fatal("expected mismatched checksum to fail")
	}
}
