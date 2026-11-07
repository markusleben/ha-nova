package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// Checkpoints of ordinary updates (calmer confirmations item 6): saved
// before the write, never at another checkpoint's cost, and gone again when
// the write never landed.

const (
	undoLockWait  = 5 * time.Second
	undoLockStale = 30 * time.Second
)

// addUndoSnapshot saves a checkpoint only when it fits (checkpointFits):
// an ordinary update's way back never costs another one.
func addUndoSnapshot(paths runtimePaths, data []byte) (snapshotSaveReceipt, error) {
	return storeUndoSnapshot(paths, data, "", true)
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

// dropUndoSnapshot removes the checkpoint addUndoSnapshot saved on server
// for a write that never landed.
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
	stack.Snapshots = slices.DeleteFunc(stack.Snapshots, func(s undoSnapshot) bool {
		return s.Server == server && s.Domain == saved.Domain && s.TargetID == saved.TargetID && s.SavedAt == saved.SavedAt
	})
	return writeJSONFile(undoSnapshotStackPath(paths), stack, 0o600)
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
