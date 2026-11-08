package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// Checkpoints of ordinary updates (calmer confirmations item 6): saved
// before the write, never at another item's checkpoint's cost, and gone
// again when the write never landed. One step per item: a calm update may
// replace only its item's own last calm step, so undo always restores the
// state before the latest calm update.

const (
	undoLockWait  = 5 * time.Second
	undoLockStale = 30 * time.Second
)

// addUndoSnapshot saves a checkpoint only when it fits (checkpointFits) or,
// for a calm record, replaces its item's own calm step (ownCalmStep).
func addUndoSnapshot(paths runtimePaths, data []byte) (snapshotSaveReceipt, error) {
	return storeUndoSnapshot(paths, data, "", storeAdd)
}

// checkpointFits: a new checkpoint for target replaces none (an untagged
// one included) and pushes no other target off this server's stack.
func checkpointFits(stack undoSnapshotStack, server, domain, target string) bool {
	key, count := cmp.Or(server, defaultServerProfileName), 0
	for _, existing := range stack.Snapshots {
		if existing.Domain == domain && existing.TargetID == target && (existing.Server == server || existing.Server == "") {
			return false
		}
		if cmp.Or(existing.Server, defaultServerProfileName) == key {
			count++
		}
	}
	return count < undoSnapshotStackLimit
}

// ownCalmStep: the next calm update of target may replace its checkpoint
// when that is its only one, a calm update's on this exact server, and live
// still equals what that update left (no edit outside HA NOVA, no undo
// since). Any doubt keeps the ask.
func ownCalmStep(stack undoSnapshotStack, server, domain, target string, live json.RawMessage) bool {
	var own []undoSnapshot
	for _, existing := range stack.Snapshots {
		if existing.Domain == domain && existing.TargetID == target && (existing.Server == server || existing.Server == "") {
			own = append(own, existing)
		}
	}
	if len(own) != 1 || !own[0].Calm || own[0].Server != server || own[0].Op != "update" || validateUndoSnapshot(own[0]) != nil {
		return false
	}
	match, err := snapshotMatchesLive(own[0], live)
	return err == nil && match
}

// dropUndoSnapshot removes the checkpoint addUndoSnapshot saved on server
// for a write that never landed, and puts back the step it replaced while
// its item has no other one.
func dropUndoSnapshot(paths runtimePaths, server string, saved snapshotSaveReceipt) error {
	unlock, err := lockUndoStack(paths)
	if err != nil {
		return err
	}
	defer unlock()
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return err
	}
	stack.SchemaVersion = undoSnapshotStackSchemaVersion
	at := slices.IndexFunc(stack.Snapshots, func(s undoSnapshot) bool { return saved.owns(s, server) })
	if at < 0 {
		return nil
	}
	stack.Snapshots = slices.Delete(stack.Snapshots, at, at+1)
	if saved.displaced != nil && !slices.ContainsFunc(stack.Snapshots, func(s undoSnapshot) bool {
		return s.Domain == saved.Domain && s.TargetID == saved.TargetID && (s.Server == server || s.Server == "")
	}) {
		stack.Snapshots = slices.Insert(stack.Snapshots, at, *saved.displaced)
	}
	return writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600)
}

// refreshUndoSnapshot gives the checkpoint addUndoSnapshot saved on server
// the read-back as its expected state while it is still that checkpoint (no
// other update replaced it meanwhile); calm false ends its replaceability.
func refreshUndoSnapshot(paths runtimePaths, server string, saved snapshotSaveReceipt, expectedAfter json.RawMessage, calm bool) error {
	unlock, err := lockUndoStack(paths)
	if err != nil {
		return err
	}
	defer unlock()
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		return err
	}
	at := slices.IndexFunc(stack.Snapshots, func(s undoSnapshot) bool { return saved.owns(s, server) })
	if at < 0 {
		return nil
	}
	snap := &stack.Snapshots[at]
	snap.ExpectedAfter, snap.Calm = redactSecrets(expectedAfter, false), snap.Calm && calm
	if err := validateUndoSnapshot(*snap); err != nil {
		return err
	}
	stack.SchemaVersion = undoSnapshotStackSchemaVersion
	return writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600)
}

// owns: s is the checkpoint this receipt saved on server.
func (saved snapshotSaveReceipt) owns(s undoSnapshot, server string) bool {
	return s.Server == server && s.Domain == saved.Domain && s.TargetID == saved.TargetID && s.SavedAt == saved.SavedAt
}

// lockUndoStack serializes each read-modify-write of the undo list across
// HA NOVA processes: a directory beside it, created atomically. One older
// than undoLockStale was left by a crash and is taken over.
func lockUndoStack(paths runtimePaths) (func(), error) {
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot lock the undo list: %w", err)
	}
	dir := undoSnapshotStackPath(paths) + ".lock"
	deadline := time.Now().Add(undoLockWait)
	for {
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return func() { _ = os.Remove(dir) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("cannot lock the undo list: %w", err)
		}
		if info, statErr := os.Stat(dir); statErr == nil && time.Since(info.ModTime()) > undoLockStale {
			_ = os.Remove(dir)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another HA NOVA command is changing the undo list; try again")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
