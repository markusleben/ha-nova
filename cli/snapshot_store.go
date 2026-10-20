package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// undoSnapshot is one client-side record of a revertible write. It stores the
// pre-write config (to restore) and the post-write verified read-back (to
// detect external drift before restoring).
//
// The store performs NO Home Assistant calls. Automation/script restores run
// through `ha-nova plan revert` + `apply`; helper restores through the skill's
// own write path — this store only reads and writes a local JSON file.
type undoSnapshot struct {
	SchemaVersion int    `json:"schema_version"`
	Op            string `json:"op"`
	Domain        string `json:"domain"`
	TargetID      string `json:"target_id"`
	// Server is the profile the update ran against (typed verbs); records
	// from `snapshot save` and older CLIs carry none.
	Server string `json:"server,omitempty"`
	// Name is the human-readable alias supplied by the skill so the owner
	// console and `snapshot show --list` stay recognizable (#483). Optional:
	// records written by older CLIs simply have none.
	Name string `json:"name,omitempty"`
	// SavedAt is stamped by the CLI at save time (RFC3339, UTC).
	SavedAt       string          `json:"saved_at,omitempty"`
	BeforeConfig  json.RawMessage `json:"before_config"`
	ExpectedAfter json.RawMessage `json:"expected_after"`
}

// snapshotSaveReceipt makes every checkpoint operation self-describing (#483):
// what was saved, when, and whether it created a new checkpoint, replaced the
// same target's previous one, or evicted the oldest entries past the stack cap.
type snapshotSaveReceipt struct {
	Action   string                 `json:"action"` // "created" or "replaced"
	Op       string                 `json:"op"`
	Domain   string                 `json:"domain"`
	TargetID string                 `json:"target_id"`
	Name     string                 `json:"name,omitempty"`
	SavedAt  string                 `json:"saved_at"`
	Coverage string                 `json:"coverage"`
	Evicted  []snapshotListingEntry `json:"evicted,omitempty"`
}

// snapshotListingEntry is one row of `snapshot show --list` and of a receipt's
// evicted list.
type snapshotListingEntry struct {
	Op       string `json:"op"`
	Domain   string `json:"domain"`
	TargetID string `json:"target_id"`
	Name     string `json:"name,omitempty"`
	Server   string `json:"server,omitempty"`
	SavedAt  string `json:"saved_at,omitempty"`
	Coverage string `json:"coverage"`
}

// snapshotCoverage states the one-step honesty limit everywhere a checkpoint
// is surfaced: only the target's LAST verified update is revertible.
const snapshotCoverage = "one step back: this target's last update only"

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

func saveUndoSnapshotBytes(paths runtimePaths, data []byte) (snapshotSaveReceipt, error) {
	return saveUndoSnapshotForServer(paths, data, "")
}

// saveUndoSnapshotForServer: a non-empty server (snapshot save --server)
// tags an untagged record and refuses one tagged for another server.
func saveUndoSnapshotForServer(paths runtimePaths, data []byte, server string) (snapshotSaveReceipt, error) {
	var snap undoSnapshot
	if err := unmarshalStrictJSON(data, "snapshot payload", &snap); err != nil {
		return snapshotSaveReceipt{}, err
	}
	if err := validateUndoSnapshot(snap); err != nil {
		return snapshotSaveReceipt{}, err
	}
	if server != "" && snap.Server != "" && snap.Server != server {
		return snapshotSaveReceipt{}, fmt.Errorf("the record is for server %q, not %q; no snapshot was written", snap.Server, server)
	}
	snap.Server = cmp.Or(snap.Server, server)
	snap.SchemaVersion = undoSnapshotSchemaVersion
	if name, _ := requestedServerSelection(); snap.Server == "" && name == demoProfileName && demoProfileOn(paths) && !realDemoProfileExists(paths) {
		snap.Server = demoProfileName // never an untagged record a real home could revert to
	}
	// The CLI clock is the single source of the stamp; a caller-supplied
	// saved_at is overwritten, never trusted.
	snap.SavedAt = snapshotNow().UTC().Format(time.RFC3339)
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return snapshotSaveReceipt{}, err
	}
	action := "created"
	kept := make([]undoSnapshot, 0, len(stack.Snapshots)+1)
	kept = append(kept, snap)
	for _, existing := range stack.Snapshots {
		if existing.Domain == snap.Domain && existing.TargetID == snap.TargetID && existing.Server == snap.Server {
			// Same-target update: the previous checkpoint is replaced — only
			// the newest state stays revertible, and the receipt says so.
			action = "replaced"
			continue
		}
		kept = append(kept, existing)
	}
	// The cap counts per server, so one house (e.g. the demo) never evicts
	// another's checkpoints.
	var evicted []snapshotListingEntry
	perServer := map[string]int{}
	capped := kept[:0]
	for _, existing := range kept {
		server := cmp.Or(existing.Server, defaultServerProfileName) // untagged: single-home records
		if perServer[server]++; perServer[server] > undoSnapshotStackLimit {
			evicted = append(evicted, snapshotListing(existing))
			continue
		}
		capped = append(capped, existing)
	}
	kept = capped
	stack = undoSnapshotStack{SchemaVersion: undoSnapshotStackSchemaVersion, Snapshots: kept}
	if err := writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600); err != nil {
		return snapshotSaveReceipt{}, fmt.Errorf("cannot write snapshot: %s", err)
	}
	// The legacy single-slot file is folded into the stack above; drop it so
	// the two stores can never disagree. Best effort — a leftover legacy file
	// only re-migrates as the oldest entry.
	if err := os.Remove(undoSnapshotPath(paths)); err != nil && !isNotExist(err) {
		printHumanWarn("legacy undo-snapshot cleanup skipped: %s", err)
	}
	return snapshotSaveReceipt{
		Action:   action,
		Op:       snap.Op,
		Domain:   snap.Domain,
		TargetID: snap.TargetID,
		Name:     snap.Name,
		SavedAt:  snap.SavedAt,
		Coverage: snapshotCoverage,
		Evicted:  evicted,
	}, nil
}

func snapshotListing(snap undoSnapshot) snapshotListingEntry {
	return snapshotListingEntry{
		Op:       snap.Op,
		Domain:   snap.Domain,
		TargetID: snap.TargetID,
		Name:     snap.Name,
		Server:   snap.Server,
		SavedAt:  snap.SavedAt,
		Coverage: snapshotCoverage,
	}
}

// snapshotNow is swappable for tests.
var snapshotNow = time.Now

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
	return selectServerUndoSnapshot(paths, "", target, domain)
}

// selectServerUndoSnapshot is selectUndoSnapshot restricted to one server
// ("" = any): another server's checkpoint for the same id is never picked;
// untagged records stay eligible (the revert drift check still compares them
// with this server's live config).
func selectServerUndoSnapshot(paths runtimePaths, server, target, domain string) (undoSnapshot, error) {
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return undoSnapshot{}, err
	}
	if server != "" {
		// This server's records plus untagged legacy ones; newest first, so
		// the first record per domain+target is the one a revert means.
		// The demo house takes only its own records: an untagged one may be
		// the user's home, and the demo is shared.
		untagged := !(server == demoProfileName && activeServerIsDemo())
		var mine []undoSnapshot
		seen := map[string]bool{}
		for _, snap := range stack.Snapshots {
			if key := snap.Domain + "\x00" + snap.TargetID; (untagged && snap.Server == "" || snap.Server == server) && !seen[key] {
				seen[key] = true
				mine = append(mine, snap)
			}
		}
		stack.Snapshots = mine
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
		return undoSnapshot{}, fmt.Errorf("target %s is ambiguous (several domains or servers); pass --domain or --server", target)
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
	// Order-aware: a reorder of variables/fields after the checkpoint is drift.
	if changes, err := configChanges(snap.ExpectedAfter, liveRaw); err != nil || len(changes) != 0 {
		return false, err
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
