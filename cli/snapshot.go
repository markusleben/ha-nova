package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// undoSnapshot is the client-side, single-slot record of the most recent
// revertible write. It stores the pre-write config (to restore) and the
// post-write verified read-back (to detect external drift before restoring).
//
// The store performs NO Home Assistant calls. The skill orchestrates the
// actual restore through `ha-nova relay` and the apply-agent, so the single
// write path and the dumb-relay contract stay intact — this command only reads
// and writes a local JSON file.
type undoSnapshot struct {
	SchemaVersion int             `json:"schema_version"`
	Op            string          `json:"op"`
	Domain        string          `json:"domain"`
	TargetID      string          `json:"target_id"`
	BeforeConfig  json.RawMessage `json:"before_config"`
	ExpectedAfter json.RawMessage `json:"expected_after"`
}

const undoSnapshotSchemaVersion = 1

// Exit codes for `snapshot verify`: 0 = live matches the post-write state,
// snapshotExitDrift = live drifted (external edit), 1 = operational error.
const snapshotExitDrift = 2

func undoSnapshotPath(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "undo-snapshot.json")
}

func runSnapshotCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova snapshot <save|show|verify> ...")
		return 1
	}
	switch args[0] {
	case "save":
		return runSnapshotSave(paths, args[1:])
	case "show":
		return runSnapshotShow(paths)
	case "verify":
		return runSnapshotVerify(paths, args[1:])
	default:
		printErr("Unknown snapshot command: %s", args[0])
		return 1
	}
}

func runSnapshotSave(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot save", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dataFile string
	fs.StringVar(&dataFile, "data-file", "", "path to the JSON record (defaults to stdin)")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	if fs.NArg() != 0 {
		printErr("snapshot save takes no positional arguments; use --data-file <file> or pipe the record on stdin")
		return 1
	}
	var data []byte
	var err error
	if strings.TrimSpace(dataFile) != "" {
		// File-based input is shell-agnostic — the canonical relay contract and
		// `ha-nova diff --before/--after` are file-based too, so the skill never
		// has to build a stdin redirect/pipe (fragile on Windows PowerShell).
		data, err = os.ReadFile(dataFile)
		if err != nil {
			printErr("cannot read snapshot record: %s", err)
			return 1
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			printErr("cannot read snapshot from stdin: %s", err)
			return 1
		}
	}
	if err := saveUndoSnapshotBytes(paths, data); err != nil {
		printErr("%s", err)
		return 1
	}
	return 0
}

func runSnapshotShow(paths runtimePaths) int {
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		printErr("cannot render snapshot: %s", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}

func runSnapshotVerify(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var against string
	fs.StringVar(&against, "against", "", "path to the current live config JSON")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	if strings.TrimSpace(against) == "" {
		printErr("--against <live.json> is required")
		return 1
	}
	snap, err := loadUndoSnapshot(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	liveRaw, err := os.ReadFile(against)
	if err != nil {
		printErr("cannot read live config: %s", err)
		return 1
	}
	match, err := snapshotMatchesLive(snap, liveRaw)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if match {
		fmt.Fprintln(os.Stdout, "match")
		return 0
	}
	fmt.Fprintln(os.Stdout, "drift")
	return snapshotExitDrift
}

// saveUndoSnapshotBytes parses, validates and atomically writes the snapshot.
// N=1: it overwrites any prior snapshot — only the last write is revertible.
// The store assumes single-flight writes: the skill's single write path
// (apply-agent) serialises operations, so concurrent saves don't race on the one
// slot. The write itself is atomic (temp file + rename), so a reader never sees a
// torn file even if that assumption is violated.
func saveUndoSnapshotBytes(paths runtimePaths, data []byte) error {
	var snap undoSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("snapshot payload is not valid JSON: %s", err)
	}
	if err := validateUndoSnapshot(snap); err != nil {
		return err
	}
	snap.SchemaVersion = undoSnapshotSchemaVersion
	if err := writeJSONFile(undoSnapshotPath(paths), snap, 0o600); err != nil {
		return fmt.Errorf("cannot write snapshot: %s", err)
	}
	return nil
}

func validateUndoSnapshot(snap undoSnapshot) error {
	if strings.TrimSpace(snap.Op) == "" {
		return fmt.Errorf("snapshot is missing op")
	}
	if strings.TrimSpace(snap.Domain) == "" {
		return fmt.Errorf("snapshot is missing domain")
	}
	if strings.TrimSpace(snap.TargetID) == "" {
		return fmt.Errorf("snapshot is missing target_id")
	}
	if !hasJSONContent(snap.BeforeConfig) {
		return fmt.Errorf("snapshot is missing before_config")
	}
	if !hasJSONContent(snap.ExpectedAfter) {
		return fmt.Errorf("snapshot is missing expected_after")
	}
	// Reject at save time what verify can never evaluate: both bodies must be
	// JSON objects. An array/scalar would pass the content check but make
	// `snapshot verify` error out (exit 1) instead of returning match/drift.
	if _, err := configObjectFromBytes(snap.BeforeConfig); err != nil {
		return fmt.Errorf("snapshot before_config is not a JSON object: %s", err)
	}
	if _, err := configObjectFromBytes(snap.ExpectedAfter); err != nil {
		return fmt.Errorf("snapshot expected_after is not a JSON object: %s", err)
	}
	return nil
}

func hasJSONContent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func loadUndoSnapshot(paths runtimePaths) (undoSnapshot, error) {
	data, err := os.ReadFile(undoSnapshotPath(paths))
	if err != nil {
		if isNotExist(err) {
			return undoSnapshot{}, fmt.Errorf("no undo snapshot available")
		}
		return undoSnapshot{}, err
	}
	var snap undoSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return undoSnapshot{}, fmt.Errorf("undo snapshot is corrupt: %s", err)
	}
	return snap, nil
}

// snapshotMatchesLive reports whether the live config still matches the stored
// post-write read-back. It compares at the CONTENT level — using the same
// normalisation as `ha-nova diff` — so HA-managed bookkeeping (id, unique_id,
// created_at, …) and singular/plural alias differences are never mistaken for
// drift. Object key order is irrelevant; array order stays significant. Only a
// real content change (an external edit) refuses the blind restore.
//
// This must stay aligned with `ha-nova diff`: if the two ever disagreed, the
// drift guard would cry wolf on every revert and train the model to ignore it.
func snapshotMatchesLive(snap undoSnapshot, liveRaw []byte) (bool, error) {
	expected, err := configObjectFromBytes(snap.ExpectedAfter)
	if err != nil {
		return false, fmt.Errorf("stored snapshot is corrupt: %s", err)
	}
	live, err := configObjectFromBytes(liveRaw)
	if err != nil {
		return false, fmt.Errorf("live config is not valid JSON: %s", err)
	}
	return len(renderConfigChanges(expected, live)) == 0, nil
}
