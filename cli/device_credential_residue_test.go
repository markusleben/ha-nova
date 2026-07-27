package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestProfileResidueCleanupRejectsAmbiguousPendingDelete(t *testing.T) {
	withDeviceStorageTestHome(t)
	service := deviceCredentialPendingServiceForProfile("cabin")
	path, err := deviceSecretFilePath(service)
	if err != nil {
		t.Fatal(err)
	}
	if err := deviceSecretFileSet(service, validCredential(77)); err != nil {
		t.Fatal(err)
	}

	originalRemove := deviceResidueRemove
	deviceResidueRemove = func(target string) error {
		if target == path {
			return nil
		}
		return originalRemove(target)
	}
	t.Cleanup(func() { deviceResidueRemove = originalRemove })

	err = removeDeviceFileStorageResidueForProfile("cabin")
	if err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("ambiguous pending delete error = %v", err)
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Fatalf("pending residue was not retained for retry: %v", statErr)
	}
}

func TestFullResidueCleanupFailsClosedOnDirectoryReadError(t *testing.T) {
	withDeviceStorageTestHome(t)
	if err := deviceSecretFileSet(
		deviceCredentialServiceForProfile("default"),
		validCredential(78),
	); err != nil {
		t.Fatal(err)
	}
	dir, err := deviceSecretFileDir()
	if err != nil {
		t.Fatal(err)
	}

	originalReadDir := deviceResidueReadDir
	deviceResidueReadDir = func(path string) ([]os.DirEntry, error) {
		if path == dir {
			return nil, errors.New("read denied")
		}
		return originalReadDir(path)
	}
	t.Cleanup(func() { deviceResidueReadDir = originalReadDir })

	err = removeAllDeviceFileStorageResidue()
	if err == nil || !strings.Contains(err.Error(), "read denied") {
		t.Fatalf("directory read failure = %v", err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("directory read failure removed the storage marker")
	}
}

func TestResidueCleanupPropagatesMarkerDeleteFailure(t *testing.T) {
	withDeviceStorageTestHome(t)
	service := deviceCredentialServiceForProfile("default")
	if err := deviceSecretFileSet(service, validCredential(79)); err != nil {
		t.Fatal(err)
	}
	if err := deviceSecretFileDelete(service); err != nil {
		t.Fatal(err)
	}
	marker, err := deviceFileBackendMarkerPath()
	if err != nil {
		t.Fatal(err)
	}

	originalRemove := deviceResidueRemove
	deviceResidueRemove = func(path string) error {
		if path == marker {
			return errors.New("marker denied")
		}
		return originalRemove(path)
	}
	t.Cleanup(func() { deviceResidueRemove = originalRemove })

	err = removeDeviceFileStorageResidueForProfile("default")
	if err == nil || !strings.Contains(err.Error(), "marker denied") {
		t.Fatalf("marker delete failure = %v", err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("failed marker delete was reported absent")
	}
}

func TestFileStorageCanaryRejectsAmbiguousMarkerDelete(t *testing.T) {
	withDeviceStorageTestHome(t)
	markerPath, err := deviceFileBackendMarkerPath()
	if err != nil {
		t.Fatal(err)
	}
	originalRemove := deviceResidueRemove
	deviceResidueRemove = func(path string) error {
		if path == markerPath {
			return nil
		}
		return originalRemove(path)
	}
	t.Cleanup(func() { deviceResidueRemove = originalRemove })

	err = fileStorageCanary()
	if err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("ambiguous marker delete was accepted: %v", err)
	}
}
