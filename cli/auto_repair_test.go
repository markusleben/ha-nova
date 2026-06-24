package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAttemptClientAutoRepair_SkipsWhenReady(t *testing.T) {
	client := clientStatus{
		ID:              "claude",
		Label:           "Claude Code",
		SupportedOnOS:   true,
		RuntimeDetected: true,
		Attached:        true,
		Ready:           true,
	}
	got := attemptClientAutoRepair(runtimePaths{}, client)
	if got.Repaired {
		t.Fatalf("expected no repair, got Repaired=true")
	}
	if !got.Skipped {
		t.Fatalf("expected Skipped=true, got Skipped=false")
	}
	if got.Err != nil {
		t.Fatalf("expected no error, got %v", got.Err)
	}
	if !strings.Contains(got.SkipReason, "ready") {
		t.Fatalf("expected SkipReason to mention 'ready', got %q", got.SkipReason)
	}
}

func TestAttemptClientAutoRepair_SkipsWhenRuntimeMissing(t *testing.T) {
	client := clientStatus{
		ID:              "claude",
		Label:           "Claude Code",
		RuntimeDetected: false,
	}
	got := attemptClientAutoRepair(runtimePaths{}, client)
	if got.Repaired {
		t.Fatalf("expected no repair, got Repaired=true")
	}
	if !got.Skipped {
		t.Fatalf("expected Skipped=true")
	}
	if !strings.Contains(got.SkipReason, "runtime") {
		t.Fatalf("expected SkipReason to mention 'runtime', got %q", got.SkipReason)
	}
}

func TestAttemptClientAutoRepair_SkipsWhenAlreadyAttached(t *testing.T) {
	client := clientStatus{
		ID:              "claude",
		Label:           "Claude Code",
		RuntimeDetected: true,
		Attached:        true,
	}
	got := attemptClientAutoRepair(runtimePaths{}, client)
	if got.Repaired {
		t.Fatalf("expected no repair, got Repaired=true")
	}
	if !got.Skipped {
		t.Fatalf("expected Skipped=true")
	}
	if !strings.Contains(got.SkipReason, "attached") {
		t.Fatalf("expected SkipReason to mention 'attached', got %q", got.SkipReason)
	}
}

func TestAttemptClientAutoRepair_SkipsOnDevBuild(t *testing.T) {
	// A dev-synced build (BuildChannel=dev) must never auto-repair, even when the
	// client looks drifted (not attached) — otherwise the session-start hook
	// clobbers the dev-synced Claude plugin with the release every session.
	orig := BuildChannel
	t.Cleanup(func() { BuildChannel = orig })
	BuildChannel = "dev"

	client := clientStatus{
		ID:              "claude",
		Label:           "Claude Code",
		RuntimeDetected: true,
		Ready:           false,
		Attached:        false,
	}
	got := attemptClientAutoRepair(runtimePaths{}, client)
	if got.Repaired {
		t.Fatal("a dev build must never be repaired (clobbered)")
	}
	if !got.Skipped {
		t.Fatalf("expected Skipped=true on a dev build, got %+v", got)
	}
	if !strings.Contains(got.SkipReason, "dev") {
		t.Fatalf("expected SkipReason to mention 'dev', got %q", got.SkipReason)
	}
}

func TestAttemptClientAutoRepair_SkipsClaudeWhenStateUnreadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	// Simulate a torn write by Claude Code: file exists but is invalid JSON.
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(`{"plugins": {"ha-nova@ha-`), 0o644); err != nil {
		t.Fatalf("write torn installed plugins: %v", err)
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	got := attemptClientAutoRepair(paths, clientStatus{ID: "claude", Label: "Claude Code", RuntimeDetected: true})
	if !got.Skipped {
		t.Fatalf("expected Skipped=true, got %+v", got)
	}
	if !strings.Contains(got.SkipReason, "unreadable") {
		t.Fatalf("expected SkipReason to mention 'unreadable', got %q", got.SkipReason)
	}
	if got.Err != nil {
		t.Fatalf("expected no error, got %v", got.Err)
	}
	if data, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("expected no claude CLI invocation for unreadable state, got:\n%s", string(data))
	}
}

func TestAttemptClientAutoRepair_SkipsClaudeWhenSnapshotHealthyAtRepairTime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeInstalledClaudePluginFixture(t, home)
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":"`+strings.ReplaceAll(filepath.Join(home, "marketplace"), `\`, `\\`)+`"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// The caller's status claims detached (e.g. computed from a stale read),
	// but the live snapshot at repair time is healthy.
	got := attemptClientAutoRepair(paths, clientStatus{ID: "claude", Label: "Claude Code", RuntimeDetected: true})
	if !got.Skipped {
		t.Fatalf("expected Skipped=true, got %+v", got)
	}
	if !strings.Contains(got.SkipReason, "attached") {
		t.Fatalf("expected SkipReason to mention 'attached', got %q", got.SkipReason)
	}
	if data, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("expected no claude CLI invocation for healthy state, got:\n%s", string(data))
	}
}

func TestRunClientAutoRepair_SkipsWhenLockHeld(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "auto-repair.lock"), nil, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	got := runClientAutoRepair(runtimePaths{ConfigDir: configDir}, []clientStatus{
		{ID: "claude", Label: "Claude Code", Ready: true, RuntimeDetected: true, Attached: true},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(got))
	}
	if !got[0].Skipped || !strings.Contains(got[0].SkipReason, "in progress") {
		t.Fatalf("expected lock skip outcome, got %+v", got[0])
	}
	if _, err := os.Stat(filepath.Join(configDir, "auto-repair.lock")); err != nil {
		t.Fatalf("expected foreign lock to stay in place, got %v", err)
	}
}

func TestRunClientAutoRepair_StealsStaleLockAndReleases(t *testing.T) {
	configDir := t.TempDir()
	lockPath := filepath.Join(configDir, "auto-repair.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	stale := time.Now().Add(-autoRepairLockStaleAfter - time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("age lock: %v", err)
	}

	got := runClientAutoRepair(runtimePaths{ConfigDir: configDir}, []clientStatus{
		{ID: "claude", Label: "Claude Code", Ready: true, RuntimeDetected: true, Attached: true},
	})
	if len(got) != 1 || !got[0].Skipped || !strings.Contains(got[0].SkipReason, "ready") {
		t.Fatalf("expected stale lock to be stolen and run to proceed, got %+v", got)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock to be released after run, got %v", err)
	}
}

func TestRunClientAutoRepair_HandlesEmptyList(t *testing.T) {
	got := runClientAutoRepair(runtimePaths{}, nil)
	if len(got) != 0 {
		t.Fatalf("expected empty outcomes, got %d", len(got))
	}
}

func TestRunClientAutoRepair_PreservesOrder(t *testing.T) {
	clients := []clientStatus{
		{ID: "codex", Label: "Codex CLI", Ready: true, RuntimeDetected: true, Attached: true},
		{ID: "antigravity", Label: "Google Antigravity CLI", Ready: true, RuntimeDetected: true, Attached: true},
	}
	got := runClientAutoRepair(runtimePaths{}, clients)
	if len(got) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(got))
	}
	if got[0].ClientID != "codex" || got[1].ClientID != "antigravity" {
		t.Fatalf("expected order codex,antigravity; got %s,%s", got[0].ClientID, got[1].ClientID)
	}
	for _, oc := range got {
		if oc.Repaired || oc.Err != nil {
			t.Fatalf("expected idempotent skip for ready client %s, got %+v", oc.ClientID, oc)
		}
	}
}
