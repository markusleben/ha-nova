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

func TestLoadClientRegistryRejectsUnknownSetupCapability(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)

	writeTestRegistry(t, root, `{"clients":[{"id":"hermes","label":"Hermes Agent","adapter_kind":"skill_tree","supported_os":["linux"],"setup":{"magic":true}}]}`)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if _, err := loadClientRegistry(paths); err == nil {
		t.Fatal("expected unknown setup capability to fail")
	}
}

func TestLoadClientRegistryRejectsUnknownAdapter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)

	writeTestRegistry(t, root, `{"clients":[{"id":"future","label":"Future Client","adapter_kind":"mystery","supported_os":["linux"]}]}`)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if _, err := loadClientRegistry(paths); err == nil {
		t.Fatal("expected unknown adapter to fail")
	}
}

func TestLoadClientRegistryValidatesServiceCredentials(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing label",
			body: `{"clients":[{"id":"hermes","label":"Hermes Agent","adapter_kind":"skill_tree","supported_os":["linux"],"setup":{"service_credentials":{"recommended_when":["headless"],"help":"Use for service sessions."}}}]}`,
		},
		{
			name: "invalid recommendation",
			body: `{"clients":[{"id":"hermes","label":"Hermes Agent","adapter_kind":"skill_tree","supported_os":["linux"],"setup":{"service_credentials":{"recommended_when":["maybe"],"label":"Service mode","help":"Use for service sessions."}}}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HA_NOVA_DEV_ROOT", root)
			writeTestRegistry(t, root, tc.body)

			paths, err := detectPaths()
			if err != nil {
				t.Fatalf("detectPaths() error: %v", err)
			}

			if _, err := loadClientRegistry(paths); err == nil {
				t.Fatal("expected invalid service credentials to fail")
			}
		})
	}
}

func TestClientServiceCredentialsHonorPerOSDisable(t *testing.T) {
	client := clientRegistryEntry{
		ID:          "future",
		Label:       "Future Client",
		AdapterKind: "skill_tree",
		SupportedOS: []string{"macos", "linux", "windows"},
		Setup: clientRegistrySetup{
			ServiceCredentials: &clientRegistryServiceCredentials{
				RecommendedWhen: []string{"headless"},
				Label:           "Service mode",
				Help:            "Use for service sessions.",
			},
		},
		PerOS: map[string]clientRegistryOS{
			bundlePlatformOS(): {
				Setup: &clientRegistrySetup{
					ServiceCredentials: &clientRegistryServiceCredentials{Disabled: true},
				},
			},
		},
	}
	if clientSupportsServiceCredentials(client) {
		t.Fatal("expected current OS override to disable service credentials")
	}
}

func TestSelectedClientsServiceCredentialHintUsesRegistryMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)

	writeTestRegistry(t, root, `{"clients":[
		{"id":"future","label":"Future Client","adapter_kind":"skill_tree","supported_os":["macos","linux","windows"],"setup":{"service_credentials":{"recommended_when":["headless"],"label":"Service mode","help":"Use for service sessions."}}},
		{"id":"plain","label":"Plain Client","adapter_kind":"skill_tree","supported_os":["macos","linux","windows"]}
	]}`)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	serviceCredentials, label, ok, err := selectedClientsServiceCredentialHint(paths, []string{"future"})
	if err != nil {
		t.Fatalf("selectedClientsServiceCredentialHint() error = %v", err)
	}
	if !ok {
		t.Fatal("expected registry-declared service credentials")
	}
	if label != "Future Client" || serviceCredentials.Label != "Service mode" {
		t.Fatalf("unexpected service credentials: label=%q credentials=%+v", label, serviceCredentials)
	}

	if _, _, ok, err := selectedClientsServiceCredentialHint(paths, []string{"plain"}); err != nil || ok {
		t.Fatalf("expected plain client not to advertise service credentials: ok=%v err=%v", ok, err)
	}
}

func TestRequireSelectedClientServiceCredentialsUsesRegistryMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", root)

	writeTestRegistry(t, root, `{"clients":[
		{"id":"future","label":"Future Client","adapter_kind":"skill_tree","supported_os":["macos","linux","windows"],"setup":{"service_credentials":{"recommended_when":["headless"],"label":"Service mode","help":"Use for service sessions."}}},
		{"id":"plain","label":"Plain Client","adapter_kind":"skill_tree","supported_os":["macos","linux","windows"]}
	]}`)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := requireSelectedClientServiceCredentials(paths, []string{"future"}); err != nil {
		t.Fatalf("expected fake registry client to support service credentials: %v", err)
	}
	if err := requireSelectedClientServiceCredentials(paths, []string{"plain"}); err == nil {
		t.Fatal("expected fake registry client without capability to fail service credentials")
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
