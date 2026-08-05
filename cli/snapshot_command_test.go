package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if _, err := saveUndoSnapshotBytes(paths, []byte(payload)); err != nil {
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

func TestSnapshotSaveReceiptContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	restore := snapshotNow
	snapshotNow = func() time.Time { return time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC) }
	defer func() { snapshotNow = restore }()

	record := func(target, name string) []byte {
		return []byte(`{"op":"update","domain":"automation","target_id":"` + target + `","name":"` + name + `","before_config":{"alias":"X"},"expected_after":{"alias":"Y"}}`)
	}

	// First save creates, stamps the CLI clock, and carries the alias.
	receipt, err := saveUndoSnapshotBytes(paths, record("t1", "Morning routine"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if receipt.Action != "created" || receipt.Name != "Morning routine" ||
		receipt.SavedAt != "2026-08-04T06:00:00Z" || receipt.Coverage == "" || len(receipt.Evicted) != 0 {
		t.Fatalf("unexpected create receipt: %+v", receipt)
	}

	// Same-target save replaces — and says so.
	receipt, err = saveUndoSnapshotBytes(paths, record("t1", "Morning routine"))
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if receipt.Action != "replaced" {
		t.Fatalf("expected replaced, got %+v", receipt)
	}

	// Filling past the cap evicts the oldest and names it in the receipt.
	for i := 2; i <= undoSnapshotStackLimit; i++ {
		if _, err := saveUndoSnapshotBytes(paths, record(fmt.Sprintf("t%d", i), "")); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	receipt, err = saveUndoSnapshotBytes(paths, record("t6", "Overflow"))
	if err != nil {
		t.Fatalf("overflow save: %v", err)
	}
	if len(receipt.Evicted) != 1 || receipt.Evicted[0].TargetID != "t1" || receipt.Evicted[0].Name != "Morning routine" {
		t.Fatalf("expected t1 evicted with alias, got %+v", receipt.Evicted)
	}

	// The listing carries name, saved time, and the one-step coverage.
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	listing := snapshotListing(stack.Snapshots[0])
	if listing.Name != "Overflow" || listing.SavedAt == "" || listing.Coverage != snapshotCoverage {
		t.Fatalf("unexpected listing: %+v", listing)
	}
}

func TestSnapshotSavePreFieldRecordStillLoads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Records written by older CLIs have neither name nor saved_at.
	receipt, err := saveUndoSnapshotBytes(paths, []byte(`{"op":"update","domain":"automation","target_id":"legacy","before_config":{"a":1},"expected_after":{"a":2}}`))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if receipt.Name != "" || receipt.Action != "created" || receipt.SavedAt == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

func TestRunSnapshotSavePrintsReceiptJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	record := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(record, []byte(`{"op":"update","domain":"automation","target_id":"cmd1","name":"Cmd Alias","before_config":{"a":1},"expected_after":{"a":2}}`), 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}

	// Capture the command's stdout — the receipt is its ONLY output contract.
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	code := runSnapshotSave(paths, []string{"--data-file", record})
	w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code %d, output %s", code, out)
	}
	var receipt snapshotSaveReceipt
	if err := json.Unmarshal(out, &receipt); err != nil {
		t.Fatalf("stdout is not the receipt JSON: %v\n%s", err, out)
	}
	if receipt.Action != "created" || receipt.TargetID != "cmd1" ||
		receipt.Name != "Cmd Alias" || receipt.SavedAt == "" || receipt.Coverage != snapshotCoverage {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

func TestSnapshotStackPreFieldStoreLoadsAndLists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A stack written by a pre-#483 CLI: no name, no saved_at anywhere.
	preField := `{"schema_version":2,"snapshots":[{"schema_version":1,"op":"update","domain":"automation","target_id":"old1","before_config":{"a":1},"expected_after":{"a":2}}]}`
	if err := os.WriteFile(undoSnapshotStackPath(paths), []byte(preField), 0o600); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatalf("pre-field store must load: %v", err)
	}
	listing := snapshotListing(stack.Snapshots[0])
	if listing.TargetID != "old1" || listing.Name != "" || listing.SavedAt != "" || listing.Coverage != snapshotCoverage {
		t.Fatalf("unexpected legacy listing: %+v", listing)
	}
	// Saving on top still works and reports a receipt.
	receipt, err := saveUndoSnapshotBytes(paths, []byte(`{"op":"update","domain":"automation","target_id":"old1","before_config":{"a":2},"expected_after":{"a":3}}`))
	if err != nil {
		t.Fatalf("save over legacy: %v", err)
	}
	if receipt.Action != "replaced" {
		t.Fatalf("expected replaced over legacy entry, got %+v", receipt)
	}
}
