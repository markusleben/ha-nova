package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSnapshotPaths(t *testing.T) runtimePaths {
	t.Helper()
	return runtimePaths{ConfigDir: t.TempDir()}
}

func validSnapshotJSON() []byte {
	return []byte(`{
		"op": "update",
		"domain": "automation",
		"target_id": "1700000000000",
		"before_config": {"alias": "Hallway", "mode": "single", "trigger": []},
		"expected_after": {"alias": "Hallway", "mode": "restart", "trigger": []}
	}`)
}

func TestSnapshotSaveShowRoundTrip(t *testing.T) {
	paths := testSnapshotPaths(t)

	if err := saveUndoSnapshotBytes(paths, validSnapshotJSON()); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if snap.SchemaVersion != undoSnapshotSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", undoSnapshotSchemaVersion, snap.SchemaVersion)
	}
	if snap.Op != "update" || snap.Domain != "automation" || snap.TargetID != "1700000000000" {
		t.Fatalf("metadata not round-tripped: %+v", snap)
	}
	if !hasJSONContent(snap.BeforeConfig) || !hasJSONContent(snap.ExpectedAfter) {
		t.Fatalf("config payloads lost in round-trip: %+v", snap)
	}

	// File is written with owner-only permissions (contains config detail).
	info, err := os.Stat(undoSnapshotStackPath(paths))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}
}

func TestSnapshotStackKeepsMultipleTargetsAndReplacesSameTarget(t *testing.T) {
	paths := testSnapshotPaths(t)
	record := func(target, before string) string {
		return `{"op":"update","domain":"automation","target_id":"` + target + `","before_config":{"alias":"` + before + `"},"expected_after":{"alias":"after"}}`
	}
	// Three targets, then a re-update of the second: 3 entries, second replaced + moved to top.
	for _, r := range []string{record("t1", "b1"), record("t2", "b2"), record("t3", "b3"), record("t2", "b2-new")} {
		if err := saveUndoSnapshotBytes(paths, []byte(r)); err != nil {
			t.Fatalf("save failed: %v", err)
		}
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatalf("load stack: %v", err)
	}
	if len(stack.Snapshots) != 3 {
		t.Fatalf("expected 3 entries (same target replaced), got %d", len(stack.Snapshots))
	}
	if stack.Snapshots[0].TargetID != "t2" || stack.Snapshots[1].TargetID != "t3" || stack.Snapshots[2].TargetID != "t1" {
		t.Fatalf("unexpected order: %s, %s, %s", stack.Snapshots[0].TargetID, stack.Snapshots[1].TargetID, stack.Snapshots[2].TargetID)
	}
	// Per-target selection returns the replaced record, newest-default returns t2.
	snap, err := selectUndoSnapshot(paths, "t1", "")
	if err != nil {
		t.Fatalf("select t1: %v", err)
	}
	if string(snap.BeforeConfig) == "" || snap.TargetID != "t1" {
		t.Fatalf("select t1 returned wrong record: %+v", snap)
	}
	if _, err := selectUndoSnapshot(paths, "missing", ""); err == nil {
		t.Fatal("expected error for unknown target")
	}
	// Same target_id in a second domain: target-only selection must refuse
	// to guess, and --domain must disambiguate.
	if err := saveUndoSnapshotBytes(paths, []byte(`{"op":"update","domain":"script","target_id":"t1","before_config":{"a":1},"expected_after":{"a":2}}`)); err != nil {
		t.Fatalf("save script t1: %v", err)
	}
	if _, err := selectUndoSnapshot(paths, "t1", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error for cross-domain target, got %v", err)
	}
	if snap, err := selectUndoSnapshot(paths, "t1", "automation"); err != nil || snap.Domain != "automation" {
		t.Fatalf("domain disambiguation failed: %+v, %v", snap, err)
	}
	if _, err := selectUndoSnapshot(paths, "", "automation"); err == nil {
		t.Fatal("expected error for --domain without --target")
	}
}

func TestSnapshotStackEvictsOldestBeyondLimit(t *testing.T) {
	paths := testSnapshotPaths(t)
	for i := 0; i < undoSnapshotStackLimit+2; i++ {
		r := `{"op":"update","domain":"automation","target_id":"t` + string(rune('a'+i)) + `","before_config":{"a":1},"expected_after":{"a":2}}`
		if err := saveUndoSnapshotBytes(paths, []byte(r)); err != nil {
			t.Fatalf("save %d failed: %v", i, err)
		}
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatalf("load stack: %v", err)
	}
	if len(stack.Snapshots) != undoSnapshotStackLimit {
		t.Fatalf("expected stack capped at %d, got %d", undoSnapshotStackLimit, len(stack.Snapshots))
	}
	if _, err := selectUndoSnapshot(paths, "ta", ""); err == nil {
		t.Fatal("oldest entry should have been evicted")
	}
}

func TestSnapshotLegacySingleFileMigratesIntoStack(t *testing.T) {
	paths := testSnapshotPaths(t)
	legacy := `{"schema_version":1,"op":"update","domain":"automation","target_id":"old","before_config":{"a":1},"expected_after":{"a":2}}`
	if err := os.WriteFile(undoSnapshotPath(paths), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	// Read path: the legacy record is visible as the newest snapshot.
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if snap.TargetID != "old" {
		t.Fatalf("legacy record not surfaced: %+v", snap)
	}
	// Write path: a new save folds the legacy record in and removes the file.
	next := `{"op":"update","domain":"automation","target_id":"new","before_config":{"b":1},"expected_after":{"b":2}}`
	if err := saveUndoSnapshotBytes(paths, []byte(next)); err != nil {
		t.Fatalf("save after legacy: %v", err)
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatalf("load stack: %v", err)
	}
	if len(stack.Snapshots) != 2 || stack.Snapshots[0].TargetID != "new" || stack.Snapshots[1].TargetID != "old" {
		t.Fatalf("legacy migration produced wrong stack: %+v", stack.Snapshots)
	}
	if _, err := os.Stat(undoSnapshotPath(paths)); !os.IsNotExist(err) {
		t.Fatal("legacy file should be removed after a stack save")
	}
}

func TestSnapshotSaveRejectsMissingFields(t *testing.T) {
	paths := testSnapshotPaths(t)
	cases := map[string]string{
		"missing op":            `{"domain":"automation","target_id":"1","before_config":{},"expected_after":{}}`,
		"missing domain":        `{"op":"update","target_id":"1","before_config":{},"expected_after":{}}`,
		"missing target_id":     `{"op":"update","domain":"automation","before_config":{},"expected_after":{}}`,
		"missing before_config": `{"op":"update","domain":"automation","target_id":"1","expected_after":{}}`,
		"null expected_after":   `{"op":"update","domain":"automation","target_id":"1","before_config":{},"expected_after":null}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := saveUndoSnapshotBytes(paths, []byte(payload)); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
			if _, err := os.Stat(undoSnapshotPath(paths)); !os.IsNotExist(err) {
				t.Fatalf("invalid payload must not leave a snapshot file behind")
			}
		})
	}
}

func TestSnapshotSaveRejectsNonObjectBodies(t *testing.T) {
	// before_config/expected_after must be JSON objects — an array or scalar would
	// pass the content check but make `snapshot verify` error (exit 1) instead of
	// returning match/drift, so a snapshot would be saved that can never be verified.
	paths := testSnapshotPaths(t)
	cases := map[string]string{
		"array before_config":   `{"op":"update","domain":"automation","target_id":"1","before_config":[1,2],"expected_after":{"a":1}}`,
		"scalar expected_after": `{"op":"update","domain":"automation","target_id":"1","before_config":{"a":1},"expected_after":5}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := saveUndoSnapshotBytes(paths, []byte(payload)); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
			if _, err := os.Stat(undoSnapshotPath(paths)); !os.IsNotExist(err) {
				t.Fatalf("non-object payload must not leave a snapshot file behind")
			}
		})
	}
}

func TestRunSnapshotSaveFromDataFile(t *testing.T) {
	// File-based input (--data-file) is shell-agnostic so the skill never builds a
	// stdin redirect/pipe (fragile on Windows PowerShell).
	paths := testSnapshotPaths(t)
	recordPath := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(recordPath, validSnapshotJSON(), 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if code := runSnapshotSave(paths, []string{"--data-file", recordPath}); code != 0 {
		t.Fatalf("expected exit 0 from --data-file save, got %d", code)
	}
	if _, err := loadUndoSnapshot(paths); err != nil {
		t.Fatalf("snapshot not written via --data-file: %v", err)
	}
}

func TestSnapshotSaveRejectsInvalidJSON(t *testing.T) {
	paths := testSnapshotPaths(t)
	if err := saveUndoSnapshotBytes(paths, []byte("{not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSnapshotSaveOverwritesSingleSlot(t *testing.T) {
	paths := testSnapshotPaths(t)

	first := []byte(`{"op":"update","domain":"automation","target_id":"AAA","before_config":{"v":1},"expected_after":{"v":2}}`)
	second := []byte(`{"op":"update","domain":"script","target_id":"BBB","before_config":{"v":3},"expected_after":{"v":4}}`)

	if err := saveUndoSnapshotBytes(paths, first); err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if err := saveUndoSnapshotBytes(paths, second); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if snap.TargetID != "BBB" || snap.Domain != "script" {
		t.Fatalf("N=1 store should hold only the latest write, got %+v", snap)
	}
}

func TestSnapshotShowMissing(t *testing.T) {
	paths := testSnapshotPaths(t)
	if _, err := loadUndoSnapshot(paths); err == nil {
		t.Fatal("expected error when no snapshot exists")
	}
}

func TestSnapshotVerifyMatchIgnoresKeyOrder(t *testing.T) {
	paths := testSnapshotPaths(t)
	if err := saveUndoSnapshotBytes(paths, validSnapshotJSON()); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Same content, keys reordered — HA normalisation must not read as drift.
	live := []byte(`{"trigger": [], "mode": "restart", "alias": "Hallway"}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if !match {
		t.Fatal("reordered-but-equal live config should match")
	}
}

func TestSnapshotVerifyDetectsDrift(t *testing.T) {
	paths := testSnapshotPaths(t)
	if err := saveUndoSnapshotBytes(paths, validSnapshotJSON()); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// A genuine external edit: mode changed away from the post-write state.
	live := []byte(`{"alias": "Hallway", "mode": "queued", "trigger": []}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if match {
		t.Fatal("a changed live config must be reported as drift")
	}
}

func TestSnapshotVerifyIgnoresHAManagedID(t *testing.T) {
	// Regression for the live-test false positive: the skill stores
	// expected_after as the filtered read-back (no `id`), but HA returns the
	// live config WITH its managed `id`. The drift check must compare content,
	// not bookkeeping — otherwise it reports "drift" on every revert and trains
	// the model to wave drift away, defeating the guard.
	snap := undoSnapshot{
		ExpectedAfter: []byte(`{"alias":"NOVA","mode":"single","triggers":[{"at":"08:30:00","trigger":"time"}]}`),
	}
	live := []byte(`{"id":"1781448604","alias":"NOVA","mode":"single","triggers":[{"at":"08:30:00","trigger":"time"}]}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if !match {
		t.Fatal("an id-only (HA bookkeeping) difference must NOT be reported as drift")
	}
}

func TestSnapshotVerifyDriftsOnHelperIdChange(t *testing.T) {
	// A storage helper's `id` is the {type}_id the revert rebuilds its update from.
	// If the helper was deleted/recreated with the same visible fields but a new
	// internal id, content compares equal — but a blind restore would target the
	// stale id and fail. An id present on both sides that differs must read as drift.
	snap := undoSnapshot{
		ExpectedAfter: []byte(`{"id":"5","name":"Sleep timer","duration":"00:30:00"}`),
	}
	if match, err := snapshotMatchesLive(snap, []byte(`{"id":"9","name":"Sleep timer","duration":"00:30:00"}`)); err != nil {
		t.Fatalf("compare failed: %v", err)
	} else if match {
		t.Fatal("a helper id change with the same visible fields must be reported as drift")
	}
	// Same id + content still matches (no false drift from the identity check).
	if match, err := snapshotMatchesLive(snap, []byte(`{"id":"5","name":"Sleep timer","duration":"00:30:00"}`)); err != nil {
		t.Fatalf("compare failed: %v", err)
	} else if !match {
		t.Fatal("same id and content must match")
	}
}

func TestSnapshotVerifyIgnoresHAEmptyOptionalFields(t *testing.T) {
	// Regression caught by live verification against real HA data: a real
	// automation carries description: "" (and an id) that the skill's filtered
	// expected_after omits. The drift guard must treat missing-vs-empty as a
	// match, otherwise it blocks revert on nearly every real automation.
	snap := undoSnapshot{
		ExpectedAfter: []byte(`{"alias":"NOVA","mode":"single","triggers":[{"at":"08:30:00","trigger":"time"}],"conditions":[],"actions":[{"action":"light.turn_on"}]}`),
	}
	live := []byte(`{"id":"1678952734096","description":"","alias":"NOVA","mode":"single","triggers":[{"at":"08:30:00","trigger":"time"}],"conditions":[],"actions":[{"action":"light.turn_on"}]}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if !match {
		t.Fatal("HA empty optional fields (description: \"\") + id must NOT be reported as drift")
	}
}

func TestSnapshotVerifyDriftsWhenNonEmptyFieldDropped(t *testing.T) {
	// Proves WHY expected_after must be the COMPLETE read-back body (write-safety.md):
	// if the skill reduces it to core fields and drops a NON-empty field HA stores
	// (e.g. max: 3), the live config legitimately differs → drift, and revert is
	// refused. The fix is keeping the whole body, NOT widening isEmptyValue.
	snap := undoSnapshot{
		ExpectedAfter: []byte(`{"alias":"NOVA","mode":"queued","triggers":[{"at":"08:30:00","trigger":"time"}]}`),
	}
	live := []byte(`{"id":"1","alias":"NOVA","mode":"queued","max":3,"triggers":[{"at":"08:30:00","trigger":"time"}]}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if match {
		t.Fatal("a non-empty field (max: 3) live but dropped from expected_after must read as drift — this is why expected_after must be the complete read-back body")
	}
}

func TestSnapshotVerifyDetectsRealDriftDespiteID(t *testing.T) {
	// The id filter must not over-suppress: a genuine external edit is still
	// drift even when the bookkeeping id is present.
	snap := undoSnapshot{
		ExpectedAfter: []byte(`{"alias":"NOVA","mode":"single","triggers":[{"at":"08:30:00","trigger":"time"}]}`),
	}
	live := []byte(`{"id":"1781448604","alias":"NOVA","mode":"single","triggers":[{"at":"09:00:00","trigger":"time"}]}`)
	match, err := snapshotMatchesLive(snap, live)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if match {
		t.Fatal("an external trigger-time edit must be reported as drift even when id is present")
	}
}

func TestSnapshotVerifyRespectsArrayOrder(t *testing.T) {
	snap := undoSnapshot{ExpectedAfter: []byte(`{"trigger":[{"id":"a"},{"id":"b"}]}`)}
	reordered := []byte(`{"trigger":[{"id":"b"},{"id":"a"}]}`)
	match, err := snapshotMatchesLive(snap, reordered)
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if match {
		t.Fatal("array order is semantically significant and must not match when reordered")
	}
}

func TestSnapshotVerifyRejectsInvalidLiveJSON(t *testing.T) {
	snap := undoSnapshot{ExpectedAfter: []byte(`{"a":1}`)}
	if _, err := snapshotMatchesLive(snap, []byte("{broken")); err == nil {
		t.Fatal("expected error for invalid live JSON")
	}
}

func TestUndoSnapshotPathUsesConfigDir(t *testing.T) {
	paths := runtimePaths{ConfigDir: "/tmp/example-config"}
	if got, want := undoSnapshotPath(paths), filepath.Join("/tmp/example-config", "undo-snapshot.json"); got != want {
		t.Fatalf("snapshot path = %q, want %q", got, want)
	}
}
