package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These exercise the actual subcommands (not just the pure helpers), because the
// skill's revert logic branches on the exact exit-code contract: 0=match,
// snapshotExitDrift(2)=drift, 1=operational error. A regression returning 1 on
// drift would be read as "operational error", not "drift" — silently unsafe.

func seedTestSnapshot(t *testing.T, paths runtimePaths, expectedAfter string) {
	t.Helper()
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	payload := `{"op":"update","domain":"automation","target_id":"1","before_config":{"alias":"X"},"expected_after":` + expectedAfter + `}`
	if err := saveUndoSnapshotBytes(paths, []byte(payload)); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
}

func TestRunSnapshotVerifyExitCodeContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	// expected_after is the filtered read-back (no HA `id`).
	seedTestSnapshot(t, paths, `{"alias":"NOVA","mode":"single"}`)

	// match: live equals expected content even with HA's managed id → exit 0.
	matchFile := filepath.Join(t.TempDir(), "match.json")
	if err := os.WriteFile(matchFile, []byte(`{"id":"1","alias":"NOVA","mode":"single"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if code := runSnapshotCommand(paths, []string{"verify", "--against", matchFile}); code != 0 {
			t.Fatalf("match: exit = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "match") {
		t.Fatalf("match: stdout = %q, want it to contain match", out)
	}

	// drift: a real external edit → exit 2 (the contract the skill reads).
	driftFile := filepath.Join(t.TempDir(), "drift.json")
	if err := os.WriteFile(driftFile, []byte(`{"id":"1","alias":"NOVA","mode":"restart"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if code := runSnapshotCommand(paths, []string{"verify", "--against", driftFile}); code != snapshotExitDrift {
			t.Fatalf("drift: exit = %d, want %d", code, snapshotExitDrift)
		}
	})
	if !strings.Contains(out, "drift") {
		t.Fatalf("drift: stdout = %q, want it to contain drift", out)
	}

	// missing --against → operational error, exit 1.
	if code := runSnapshotCommand(paths, []string{"verify"}); code != 1 {
		t.Fatalf("missing --against: exit = %d, want 1", code)
	}
}

func TestRunSnapshotVerifyMissingSnapshotIsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	liveFile := filepath.Join(t.TempDir(), "live.json")
	if err := os.WriteFile(liveFile, []byte(`{"alias":"X"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// No snapshot stored → exit 1, NOT drift (2): absence is an error, not drift.
	if code := runSnapshotCommand(paths, []string{"verify", "--against", liveFile}); code != 1 {
		t.Fatalf("missing snapshot: exit = %d, want 1", code)
	}
}

func TestRunSnapshotSaveReadsStdinRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	payload := `{"op":"update","domain":"automation","target_id":"42","before_config":{"alias":"A"},"expected_after":{"alias":"B"}}`
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })

	if code := runSnapshotCommand(paths, []string{"save"}); code != 0 {
		t.Fatalf("save: exit = %d, want 0", code)
	}
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if snap.TargetID != "42" {
		t.Fatalf("saved snapshot target_id = %q, want 42", snap.TargetID)
	}
}

func TestRunSnapshotSaveExplainsEmptyStdin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin })

	exitCode, output := captureCommandOutput(t, func() int {
		return runSnapshotCommand(paths, []string{"save"})
	})
	if exitCode != 1 {
		t.Fatalf("save empty stdin exit = %d, want 1", exitCode)
	}
	if !strings.Contains(output, "snapshot save requires --data-file <record-file> or JSON on stdin") {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func TestRunDiffCommandContract(t *testing.T) {
	// Missing a required flag → exit 1.
	if code := runDiffCommand(runtimePaths{}, []string{"--before", "only.json"}); code != 1 {
		t.Fatalf("missing --after: exit = %d, want 1", code)
	}

	dir := t.TempDir()
	before := filepath.Join(dir, "before.json")
	after := filepath.Join(dir, "after.json")
	if err := os.WriteFile(before, []byte(`{"mode":"single"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(after, []byte(`{"mode":"restart"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if code := runDiffCommand(runtimePaths{}, []string{"--before", before, "--after", after}); code != 0 {
			t.Fatalf("diff: exit = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "| Mode | single | restart |") {
		t.Fatalf("diff stdout = %q, want the deterministic change line", out)
	}

	outPath := filepath.Join(dir, "diff.txt")
	out = captureStdout(t, func() {
		if code := runDiffCommand(runtimePaths{}, []string{"--before", before, "--after", after, "--out", outPath}); code != 0 {
			t.Fatalf("diff --out: exit = %d, want 0", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("diff --out stdout = %q, want empty", out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "| Mode | single | restart |\n"; got != want {
		t.Fatalf("diff file = %q, want %q", got, want)
	}
}
