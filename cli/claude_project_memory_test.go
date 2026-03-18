package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUninstallWarnsAboutClaudeProjectMemoryArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	projectMemoryDir := filepath.Join(home, ".claude", "projects", "project-a", "memory")
	if err := os.MkdirAll(projectMemoryDir, 0o755); err != nil {
		t.Fatalf("mkdir project memory: %v", err)
	}
	skillMemoryPath := filepath.Join(projectMemoryDir, "ha-nova-skills.md")
	if err := os.WriteFile(skillMemoryPath, []byte(`# HA NOVA Skill System`), 0o644); err != nil {
		t.Fatalf("write ha-nova-skills.md: %v", err)
	}
	memoryPath := filepath.Join(projectMemoryDir, "MEMORY.md")
	memoryContent := `# HA NOVA Project Memory

## HA NOVA Skill System (see [ha-nova-skills.md](ha-nova-skills.md))
- Invocation: ha-nova:read
- Relay CLI: ~/.config/ha-nova/relay
`
	if err := os.WriteFile(memoryPath, []byte(memoryContent), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	if _, err := os.Stat(skillMemoryPath); err != nil {
		t.Fatalf("expected ha-nova project memory file to remain, got %v", err)
	}
	if _, err := os.Stat(memoryPath); err != nil {
		t.Fatalf("expected dedicated HA NOVA MEMORY.md to remain, got %v", err)
	}
	if strings.Count(output, "Claude project memory may still mention HA NOVA") < 2 {
		t.Fatalf("expected uninstall output to warn for both Claude project-memory files:\n%s", output)
	}
}

func TestRunUninstallKeepsMixedClaudeProjectMemoryAndWarns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	projectMemoryDir := filepath.Join(home, ".claude", "projects", "project-a", "memory")
	if err := os.MkdirAll(projectMemoryDir, 0o755); err != nil {
		t.Fatalf("mkdir project memory: %v", err)
	}
	memoryPath := filepath.Join(projectMemoryDir, "MEMORY.md")
	original := `# Project Memory

## HA NOVA Skill System (see [ha-nova-skills.md](ha-nova-skills.md))
- Invocation: ha-nova:read

## Keep Me
- unrelated project note
`
	if err := os.WriteFile(memoryPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	updated, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if string(updated) != original {
		t.Fatalf("expected mixed MEMORY.md to stay untouched:\n%s", string(updated))
	}
	if !strings.Contains(output, "Claude project memory may still mention HA NOVA") {
		t.Fatalf("expected warning about mixed Claude project memory:\n%s", output)
	}
}

func TestRunUninstallContinuesWhenClaudeProjectMemoryCleanupFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	for _, path := range []string{
		paths.InstallRoot,
		filepath.Dir(paths.PublicBinary),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}

	originalCleanup := removeClaudeProjectMemoryForUninstall
	defer func() {
		removeClaudeProjectMemoryForUninstall = originalCleanup
	}()
	removeClaudeProjectMemoryForUninstall = func(_ string, _ *uninstallReport) error {
		return errors.New("claude memory locked")
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Could not inspect Claude project memory: claude memory locked") {
		t.Fatalf("expected Claude memory warning:\n%s", output)
	}
	if !strings.Contains(output, "HA NOVA removed") {
		t.Fatalf("expected uninstall to finish despite Claude memory warning:\n%s", output)
	}
}
