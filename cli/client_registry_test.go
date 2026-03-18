package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestRegistry(t *testing.T, root, body string) {
	t.Helper()

	registryPath := filepath.Join(root, "clients", "registry.json")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	if err := os.WriteFile(registryPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func TestLoadClientRegistryRejectsInvalidEntry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)

	writeTestRegistry(t, root, `{"clients":[{"id":"claude","label":"","adapter_kind":"plugin_marketplace","supported_os":["macos"]}]}`)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if _, err := loadClientRegistry(paths); err == nil {
		t.Fatal("expected invalid registry entry to fail")
	}
}

func TestClientSupportedOnCurrentOSHonorsRegistryOSList(t *testing.T) {
	client := clientRegistryEntry{
		ID:          "future",
		Label:       "Future Client",
		AdapterKind: "skill_tree",
		SupportedOS: []string{"linux"},
	}
	if bundlePlatformOS() == "linux" {
		t.Skip("linux host would not exercise unsupported-os behavior")
	}
	if clientSupportedOnCurrentOS(client) {
		t.Fatal("expected client to be unsupported on current OS")
	}
}

func TestInstallClientFailsOnUnknownAdapterKind(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})

	writeTestRegistry(t, root, `{"clients":[{"id":"claude","label":"Claude Code","adapter_kind":"mystery","supported_os":["macos","linux","windows"]}]}`)

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if _, err := installClient(paths, root, "claude"); err == nil {
		t.Fatal("expected unknown adapter kind to fail")
	}
}
