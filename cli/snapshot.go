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

// undoSnapshot is one client-side record of a revertible write. It stores the
// pre-write config (to restore) and the post-write verified read-back (to
// detect external drift before restoring).
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

// undoSnapshotStack keeps the most recent revertible updates, newest first —
// one entry per domain+target (a re-update of the same target replaces its
// entry; reverting an update always restores that target's LAST before-state).
// Bounded so a multi-target logical change stays fully revertible without the
// store growing forever (issue #282; previously a single slot).
type undoSnapshotStack struct {
	SchemaVersion int            `json:"schema_version"`
	Snapshots     []undoSnapshot `json:"snapshots"`
}

const undoSnapshotStackSchemaVersion = 2
const undoSnapshotStackLimit = 5

// Exit codes for `snapshot verify`: 0 = live matches the post-write state,
// snapshotExitDrift = live drifted (external edit), 1 = operational error.
const snapshotExitDrift = 2

// undoSnapshotPath is the legacy single-slot store; still read for migration.
func undoSnapshotPath(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "undo-snapshot.json")
}

func undoSnapshotStackPath(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "undo-snapshots.json")
}

func runSnapshotCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova snapshot <save|show|verify> ...")
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Println("Usage: ha-nova snapshot <save|show|verify> ...")
		fmt.Println("Run 'ha-nova snapshot <subcommand> --help' to see that subcommand's flags.")
		return 0
	}
	switch args[0] {
	case "save":
		return runSnapshotSave(paths, args[1:])
	case "show":
		return runSnapshotShow(paths, args[1:])
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
	var dataFileSet bool
	fs.StringVar(&dataFile, "data-file", "", "path to the JSON record (defaults to stdin)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot save [--data-file <file>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "data-file" {
			dataFileSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot save takes no positional arguments; use --data-file <file> or pipe the record on stdin")
		return 1
	}
	var data []byte
	var err error
	if dataFileSet {
		if strings.TrimSpace(dataFile) == "" {
			printErr("--data-file requires a non-empty path; no snapshot was written")
			return 1
		}
		// File-based input is shell-agnostic — the canonical relay contract and
		// `ha-nova diff --before/--after` are file-based too, so the skill never
		// has to build a stdin redirect/pipe (fragile on Windows PowerShell).
		data, err = os.ReadFile(filepath.Clean(dataFile))
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
		if strings.TrimSpace(string(data)) == "" {
			printErr("snapshot save requires --data-file <record-file> or JSON on stdin")
			return 1
		}
	}
	if err := saveUndoSnapshotBytes(paths, data); err != nil {
		printErr("%s", err)
		return 1
	}
	return 0
}

func runSnapshotShow(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var target, domain string
	var list bool
	fs.StringVar(&target, "target", "", "select the snapshot for this target_id (default: newest)")
	fs.StringVar(&domain, "domain", "", "optional domain filter when selecting by target")
	fs.BoolVar(&list, "list", false, "list the stored snapshots (op/domain/target_id, newest first)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot show [--list] [--target <target_id>] [--domain <domain>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	var targetSet, domainSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "target":
			targetSet = true
		case "domain":
			domainSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot show does not accept positional arguments")
		return 1
	}
	if targetSet && strings.TrimSpace(target) == "" {
		printErr("--target requires a non-empty target_id")
		return 1
	}
	if domainSet && strings.TrimSpace(domain) == "" {
		printErr("--domain requires a non-empty domain")
		return 1
	}
	if domainSet && !targetSet {
		printErr("--domain requires --target")
		return 1
	}
	if list && (targetSet || domainSet) {
		printErr("--list cannot be combined with --target or --domain")
		return 1
	}
	if list {
		stack, err := loadUndoSnapshotStack(paths)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		type entry struct {
			Op       string `json:"op"`
			Domain   string `json:"domain"`
			TargetID string `json:"target_id"`
		}
		entries := make([]entry, 0, len(stack.Snapshots))
		for _, s := range stack.Snapshots {
			entries = append(entries, entry{Op: s.Op, Domain: s.Domain, TargetID: s.TargetID})
		}
		out, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			printErr("cannot render snapshot list: %s", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, string(out))
		return 0
	}
	snap, err := selectUndoSnapshot(paths, target, domain)
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
	var against, target, domain string
	fs.StringVar(&against, "against", "", "path to the current live config JSON")
	fs.StringVar(&target, "target", "", "select the snapshot for this target_id (default: newest)")
	fs.StringVar(&domain, "domain", "", "optional domain filter when selecting by target")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot verify --against <live.json> [--target <target_id>] [--domain <domain>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	var targetSet, domainSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "target":
			targetSet = true
		case "domain":
			domainSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot verify does not accept positional arguments")
		return 1
	}
	if targetSet && strings.TrimSpace(target) == "" {
		printErr("--target requires a non-empty target_id")
		return 1
	}
	if domainSet && strings.TrimSpace(domain) == "" {
		printErr("--domain requires a non-empty domain")
		return 1
	}
	if domainSet && !targetSet {
		printErr("--domain requires --target")
		return 1
	}
	if strings.TrimSpace(against) == "" {
		printErr("--against <live.json> is required")
		return 1
	}
	snap, err := selectUndoSnapshot(paths, target, domain)
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

// saveUndoSnapshotBytes parses, validates and atomically writes the snapshot
// onto the bounded stack: newest first, one entry per domain+target (same
// target replaces its entry), oldest evicted beyond the limit. The store
// assumes single-flight writes: the skill's single write path (apply-agent)
// serialises operations. The write itself is atomic (temp file + rename), so a
// reader never sees a torn file even if that assumption is violated.
func saveUndoSnapshotBytes(paths runtimePaths, data []byte) error {
	var snap undoSnapshot
	if err := unmarshalStrictJSON(data, "snapshot payload", &snap); err != nil {
		return err
	}
	if err := validateUndoSnapshot(snap); err != nil {
		return err
	}
	snap.SchemaVersion = undoSnapshotSchemaVersion
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return err
	}
	kept := make([]undoSnapshot, 0, len(stack.Snapshots)+1)
	kept = append(kept, snap)
	for _, existing := range stack.Snapshots {
		if existing.Domain == snap.Domain && existing.TargetID == snap.TargetID {
			continue
		}
		kept = append(kept, existing)
	}
	if len(kept) > undoSnapshotStackLimit {
		kept = kept[:undoSnapshotStackLimit]
	}
	stack = undoSnapshotStack{SchemaVersion: undoSnapshotStackSchemaVersion, Snapshots: kept}
	if err := writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600); err != nil {
		return fmt.Errorf("cannot write snapshot: %s", err)
	}
	// The legacy single-slot file is folded into the stack above; drop it so
	// the two stores can never disagree. Best effort — a leftover legacy file
	// only re-migrates as the oldest entry.
	if err := os.Remove(undoSnapshotPath(paths)); err != nil && !isNotExist(err) {
		printHumanWarn("legacy undo-snapshot cleanup skipped: %s", err)
	}
	return nil
}

// loadUndoSnapshotStack reads the stack store; a pre-stack single-slot file
// migrates as a one-element stack so an update written by an older CLI stays
// revertible after upgrading.
func loadUndoSnapshotStack(paths runtimePaths) (undoSnapshotStack, error) {
	data, err := os.ReadFile(undoSnapshotStackPath(paths))
	if err == nil {
		var stack undoSnapshotStack
		if err := unmarshalStrictJSON(data, "undo snapshot store", &stack); err != nil {
			return undoSnapshotStack{}, fmt.Errorf("undo snapshot store is corrupt: %s", err)
		}
		return stack, nil
	}
	if !isNotExist(err) {
		return undoSnapshotStack{}, err
	}
	legacy, err := os.ReadFile(undoSnapshotPath(paths))
	if err != nil {
		if isNotExist(err) {
			return undoSnapshotStack{SchemaVersion: undoSnapshotStackSchemaVersion}, nil
		}
		return undoSnapshotStack{}, err
	}
	var snap undoSnapshot
	if err := unmarshalStrictJSON(legacy, "legacy undo snapshot", &snap); err != nil {
		return undoSnapshotStack{}, fmt.Errorf("undo snapshot is corrupt: %s", err)
	}
	return undoSnapshotStack{
		SchemaVersion: undoSnapshotStackSchemaVersion,
		Snapshots:     []undoSnapshot{snap},
	}, nil
}

// selectUndoSnapshot picks the newest snapshot, or the one for an explicit
// target (optionally narrowed by domain).
func selectUndoSnapshot(paths runtimePaths, target, domain string) (undoSnapshot, error) {
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return undoSnapshot{}, err
	}
	if len(stack.Snapshots) == 0 {
		return undoSnapshot{}, fmt.Errorf("no undo snapshot available")
	}
	target = strings.TrimSpace(target)
	domain = strings.TrimSpace(domain)
	if target == "" {
		if domain != "" {
			return undoSnapshot{}, fmt.Errorf("--domain requires --target")
		}
		return stack.Snapshots[0], nil
	}
	var matches []undoSnapshot
	for _, snap := range stack.Snapshots {
		if snap.TargetID != target {
			continue
		}
		if domain != "" && snap.Domain != domain {
			continue
		}
		matches = append(matches, snap)
	}
	switch len(matches) {
	case 0:
		return undoSnapshot{}, fmt.Errorf("no undo snapshot for target %s", target)
	case 1:
		return matches[0], nil
	default:
		// Same target_id in more than one domain (save keys on domain+target,
		// so this is legal): never guess which config to restore.
		return undoSnapshot{}, fmt.Errorf("target %s is ambiguous across domains; pass --domain", target)
	}
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

// loadUndoSnapshot returns the newest stored snapshot (legacy call sites and
// tests; selection lives in selectUndoSnapshot).
func loadUndoSnapshot(paths runtimePaths) (undoSnapshot, error) {
	return selectUndoSnapshot(paths, "", "")
}

// snapshotMatchesLive reports whether the live config still matches the stored
// post-write read-back. It compares at the CONTENT level — using the same
// normalisation as `ha-nova diff` — so volatile HA bookkeeping (created_at,
// last_triggered, …) and singular/plural alias differences are never mistaken for
// drift. Object key order is irrelevant; array order stays significant.
//
// Identity is the exception: a storage helper's `id` is the `{type}_id` the revert
// rebuilds its update from, so if the target was deleted/recreated with the same
// visible fields but a new internal id, the content compares equal yet a blind
// restore would apply to a stale id and fail. When `id`/`unique_id` are present on
// BOTH sides and differ, treat it as drift. A key the stored snapshot omitted
// (older/filtered read-back) can't be compared, so it is never treated as drift.
func snapshotMatchesLive(snap undoSnapshot, liveRaw []byte) (bool, error) {
	expected, err := configObjectFromBytes(snap.ExpectedAfter)
	if err != nil {
		return false, fmt.Errorf("stored snapshot is corrupt: %s", err)
	}
	live, err := configObjectFromBytes(liveRaw)
	if err != nil {
		return false, fmt.Errorf("live config is not valid JSON: %s", err)
	}
	if len(renderConfigChanges(expected, live)) != 0 {
		return false, nil
	}
	return identityKeysMatch(expected, live), nil
}

// identityKeysMatch reports whether the identity keys agree. Only a value present
// on both sides that differs counts as a mismatch — a key the snapshot omitted is
// uncomparable, not drift (preserves the HA-managed-id behaviour where a filtered
// snapshot has no id but the live config carries one).
func identityKeysMatch(expected, live map[string]interface{}) bool {
	for _, k := range []string{"id", "unique_id"} {
		ev, eok := expected[k]
		lv, lok := live[k]
		if eok && lok && !valuesEqual(ev, lv) {
			return false
		}
	}
	return true
}
