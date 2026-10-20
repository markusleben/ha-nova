package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type lostDeviceKeySlots struct{ current, pending bool }

// lostDeviceKeyError: a server's config still names its Relay pairing, but
// the key that pairing needs is gone from this computer (a reset keyring, a
// copied config). HA NOVA can't sign this computer out of Home Assistant
// then, so a full purge stops until the owner says yes in the guided
// uninstall (confirmLostDeviceKeyPurge).
type lostDeviceKeyError struct {
	profile string
	slots   lostDeviceKeySlots
	cloud   bool // a Link profile keeps its own recovery; no guided finish
}

func (e *lostDeviceKeyError) Error() string {
	if e.slots.pending {
		return fmt.Sprintf(
			"pending device endpoint for server %q indicates a possibly activated pairing, but its pending credential is missing; refusing cleanup because that relay pairing cannot be authenticated for revocation",
			e.profile,
		)
	}
	return fmt.Sprintf(
		"device endpoint for server %q indicates an active pairing, but its current credential is missing; refusing cleanup because that relay pairing cannot be authenticated for revocation",
		e.profile,
	)
}

const (
	lostDeviceKeyText     = "This computer's HA NOVA key is gone, so HA NOVA can't sign this computer out of Home Assistant."
	lostDeviceKeyHandStep = "In Home Assistant, open the NOVA page and revoke this computer."
)

// lostDeviceKeyErrorText is the plain stop for runs that can't ask (--yes,
// no terminal): one next step, the run that asks.
func lostDeviceKeyErrorText(err error) (string, bool) {
	var lost *lostDeviceKeyError
	if !errors.As(err, &lost) || lost.cloud {
		return "", false
	}
	return lostDeviceKeyText + " Next step: run ha-nova uninstall (without --yes) and pick Remove.", true
}

// findLostDeviceKeys maps each server whose key is lost to its lost slots.
// ok=false when any server fails a check for another reason or is a Link
// profile: the purge then stops with that reason, as before.
func findLostDeviceKeys(paths runtimePaths) (map[string]lostDeviceKeySlots, bool) {
	// Pinned: the same storage the purge reads, never a wrong one.
	targets, _, release, err := pinnedPurgeTargets(paths)
	if err != nil {
		return nil, false
	}
	defer release()
	found := map[string]lostDeviceKeySlots{}
	for _, target := range targets {
		state, err := inspectProfilePurgeSlotState(target)
		if err != nil {
			return nil, false
		}
		err = validateRequiredProfilePurgeSlots(target, state)
		var lost *lostDeviceKeyError
		switch {
		case err == nil:
		case errors.As(err, &lost) && !lost.cloud:
			found[target.name] = lost.slots
		default:
			return nil, false
		}
	}
	return found, true
}

// confirmLostDeviceKeyPurge asks before a guided full removal goes on without
// signing this computer out. After a yes the lost pairing is forgotten in
// config.json (forgotten=true), so the purge, and a Windows helper or a
// retry, removes the rest as usual. exitCode >= 0 ends the run (cancel, error).
func confirmLostDeviceKeyPurge(out io.Writer, paths runtimePaths, mode uninstallMode) (forgotten bool, exitCode int) {
	if mode != uninstallModePurge {
		return false, -1
	}
	lost, ok := findLostDeviceKeys(paths)
	if !ok || len(lost) == 0 {
		return false, -1
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", lostDeviceKeyText)
	if names := lostDeviceKeyServerNames(lost); names != "" {
		fmt.Fprintf(out, "  Servers: %s.\n", names)
	}
	fmt.Fprintf(out, "  %s\n", lostDeviceKeyHandStep)
	spec := setupMenuSpec{
		Title:        "Finish removing HA NOVA from this computer?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		DefaultValue: "cancel",
		Options: []setupMenuOption{
			{Value: "finish", Label: "Yes, finish removing"},
			{Value: "cancel", Label: "Cancel"},
		},
	}
	answer, err := promptSetupMenu(nil, out, spec, func() (string, error) {
		line, err := promptLine("Finish removing HA NOVA from this computer? 1) Yes, finish removing  2) Cancel", "2")
		if strings.TrimSpace(line) == "1" {
			return "finish", err
		}
		return "cancel", err
	})
	if err == nil && answer == "finish" {
		err = forgetLostDeviceKeys(paths, lost)
		if err == nil {
			return true, -1
		}
	}
	if err != nil && !errors.Is(err, errSetupExit) {
		printHumanErr("%s", err)
		return false, 1
	}
	printHumanInfo("Uninstall cancelled")
	return false, 0
}

// lostDeviceKeyServerNames names the servers when it isn't just the default.
func lostDeviceKeyServerNames(lost map[string]lostDeviceKeySlots) string {
	if _, only := lost[defaultServerProfileName]; only && len(lost) == 1 {
		return ""
	}
	names := make([]string, 0, len(lost))
	for name := range lost {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// forgetLostDeviceKeys drops the endpoints of the lost slots the owner agreed
// to, under the client lock and only while they are still lost exactly so:
// a key that came back keeps its revoke.
func forgetLostDeviceKeys(paths runtimePaths, agreed map[string]lostDeviceKeySlots) error {
	return withClientMutationLock(paths, func() error {
		lost, ok := findLostDeviceKeys(paths)
		if !ok {
			return nil // the purge stops with the reason
		}
		for name, slots := range lost {
			if agreed[name] != slots {
				continue
			}
			doc, err := loadConfigDocument(paths.ConfigFile)
			if err != nil {
				return err
			}
			cfg, found := doc.flatProfile(name)
			if !found {
				return fmt.Errorf("cannot read server %q in config.json", name)
			}
			if slots.current {
				cfg.RelaySecureBaseURL, cfg.RelaySpkiPin = "", ""
			}
			if slots.pending {
				cfg.PendingSecureBaseURL, cfg.PendingSpkiPin = "", ""
			}
			top, err := doc.withProfile(name, cfg)
			if err != nil {
				return err
			}
			if err := writeJSONFileIfUnchanged(paths.ConfigFile, top, 0o600, doc.source); err != nil {
				return fmt.Errorf("forget the lost pairing of server %q: %w", name, err)
			}
		}
		return nil
	})
}
