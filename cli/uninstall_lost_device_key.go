package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// lostDeviceKey: a server's config still names its Relay pairing (the
// endpoints the owner was shown), but this computer's storage no longer holds
// the key for it (a reset keyring, a copied config). HA NOVA can't sign this
// computer out of Home Assistant then.
type lostDeviceKey struct {
	current, pending                     bool
	secureBaseURL, spkiPin               string
	pendingSecureBaseURL, pendingSpkiPin string
}

func (k lostDeviceKey) lost() bool { return k.current || k.pending }

// acceptedLostDeviceKeys is this run's yes to finishing without signing out
// (guided uninstall). The purge skips exactly these lost keys and stops on
// anything else. Nothing is written: a rerun asks again, and a key that comes
// back is revoked as usual. The Windows helper gets the yes as
// --accept-lost-keys.
var acceptedLostDeviceKeys map[string]lostDeviceKey

// lostDeviceKeyOf: the config names the slot's endpoint, the slot is empty,
// and nothing proves it was revoked or was already absent when a server
// removal was checkpointed (that removal was confirmed with the slot gone).
func lostDeviceKeyOf(
	target profilePurgeTarget,
	state profilePurgeSlotState,
) lostDeviceKey {
	interruptedFirstPromotion :=
		state.pendingExists &&
			state.pendingLocal &&
			state.pendingEndpointLive &&
			target.secureBaseURL == target.pendingSecureBaseURL &&
			target.spkiPin == target.pendingSpkiPin
	return lostDeviceKey{
		pending: (target.pendingSecureBaseURL != "" ||
			target.pendingSpkiPin != "") &&
			!state.pendingExists &&
			target.revokedPendingID == "" &&
			!(target.removalSlotsVerified && !target.pendingSlotPresent && !state.keyMayExistElsewhere),
		current: (target.secureBaseURL != "" || target.spkiPin != "") &&
			!state.currentExists &&
			target.revokedCurrentID == "" &&
			!interruptedFirstPromotion &&
			!(target.removalSlotsVerified && !target.currentSlotPresent && !state.keyMayExistElsewhere),
		secureBaseURL:        target.secureBaseURL,
		spkiPin:              target.spkiPin,
		pendingSecureBaseURL: target.pendingSecureBaseURL,
		pendingSpkiPin:       target.pendingSpkiPin,
	}
}

type lostDeviceKeyError struct {
	profile string
	key     lostDeviceKey
	// askable: a yes may finish. Not for a Link profile (its own recovery) or
	// when the other store may still hold the key.
	askable bool
}

func (e *lostDeviceKeyError) Error() string {
	if e.key.pending {
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

const lostDeviceKeyText = "This computer's HA NOVA key is gone, so HA NOVA can't sign this computer out of Home Assistant."

func lostDeviceKeyHandStep() string {
	return fmt.Sprintf("If “%s” is still listed on the NOVA page in Home Assistant, click Revoke next to it.", hostLabel())
}

// lostDeviceKeyErrorText is the plain stop for runs that can't ask (--yes,
// no terminal): one next step, the run that asks.
func lostDeviceKeyErrorText(err error) (string, bool) {
	var lost *lostDeviceKeyError
	if !errors.As(err, &lost) || !lost.askable {
		return "", false
	}
	return lostDeviceKeyText + " Next step: in a terminal on this computer, run ha-nova uninstall --purge and choose to finish removing.", true
}

// findLostDeviceKeys maps each server whose lost key a yes may skip. Other
// stops are left to the purge, which reports them.
func findLostDeviceKeys(paths runtimePaths) map[string]lostDeviceKey {
	// Pinned: the same storage the purge reads, never a wrong one.
	targets, _, release, err := pinnedPurgeTargets(paths)
	if err != nil {
		return nil
	}
	defer release()
	found := map[string]lostDeviceKey{}
	for _, target := range targets {
		state, err := inspectProfilePurgeSlotState(target)
		if err != nil {
			continue
		}
		var lost *lostDeviceKeyError
		if err := validateRequiredProfilePurgeSlots(target, state); errors.As(err, &lost) && lost.askable {
			found[target.name] = lost.key
		}
	}
	return found
}

// confirmLostDeviceKeyPurge asks before a full removal goes on without
// signing this computer out. accepted: the owner said yes (the purge then
// skips those keys). exitCode >= 0 ends the run (cancel, error).
func confirmLostDeviceKeyPurge(out io.Writer, paths runtimePaths, mode uninstallMode) (accepted bool, exitCode int) {
	if mode != uninstallModePurge {
		return false, -1
	}
	lost := findLostDeviceKeys(paths)
	if len(lost) == 0 {
		return false, -1
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", lostDeviceKeyText)
	if names := lostDeviceKeyServerNames(lost); names != "" {
		fmt.Fprintf(out, "  Servers: %s.\n", names)
	}
	fmt.Fprintf(out, "  %s\n", lostDeviceKeyHandStep())
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
	switch {
	case err != nil && !errors.Is(err, errSetupExit):
		printHumanErr("%s", err)
		return false, 1
	case err == nil && answer == "finish":
		acceptedLostDeviceKeys = lost
		return true, -1
	}
	printHumanInfo("Uninstall cancelled")
	return false, 0
}

// fingerprint names exactly what the owner said yes to, for the helper.
func (k lostDeviceKey) fingerprint() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", k)))
	return hex.EncodeToString(sum[:8])
}

// acceptLostDeviceKeysInHelper re-finds the lost keys in the Windows helper
// and accepts only those still exactly as the parent's owner saw them.
func acceptLostDeviceKeysInHelper(paths runtimePaths, value string) {
	if value == "" {
		return
	}
	lost := findLostDeviceKeys(paths)
	accepted := map[string]lostDeviceKey{}
	for _, pair := range strings.Split(value, ",") {
		name, fingerprint, _ := strings.Cut(pair, "=")
		if key, ok := lost[name]; ok && key.fingerprint() == fingerprint {
			accepted[name] = key
		}
	}
	acceptedLostDeviceKeys = accepted
}

// lostDeviceKeyArg is the --accept-lost-keys value: name=fingerprint pairs.
func lostDeviceKeyArg(keys map[string]lostDeviceKey) string {
	pairs := make([]string, 0, len(keys))
	for _, name := range lostDeviceKeyNames(keys) {
		pairs = append(pairs, name+"="+keys[name].fingerprint())
	}
	return strings.Join(pairs, ",")
}

func lostDeviceKeyNames(keys map[string]lostDeviceKey) []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lostDeviceKeyServerNames names the servers when it isn't just the default.
func lostDeviceKeyServerNames(lost map[string]lostDeviceKey) string {
	if _, only := lost[defaultServerProfileName]; only && len(lost) == 1 {
		return ""
	}
	return strings.Join(lostDeviceKeyNames(lost), ", ")
}

// serverRemoveLostKey is the lost key a typed `server remove` confirmation
// may leave behind; the command says so before it asks.
func serverRemoveLostKey(doc *configDocument, name string) (lostDeviceKey, bool) {
	cfg, _ := doc.flatProfile(name)
	target, err := profilePurgeTargetFromConfig(name, cfg)
	if err != nil {
		return lostDeviceKey{}, false
	}
	state, err := inspectProfilePurgeSlotState(target)
	if err != nil {
		return lostDeviceKey{}, false
	}
	var lost *lostDeviceKeyError
	if errors.As(validateRequiredProfilePurgeSlots(target, state), &lost) && lost.askable {
		return lost.key, true
	}
	return lostDeviceKey{}, false
}

// serverRemovalLeftLostKey: the removal checkpoint recorded a paired slot as
// already gone, so the device may still be listed on the NOVA page (shown
// again when an interrupted removal resumes).
func serverRemovalLeftLostKey(name string, cfg runtimeConfig) bool {
	target, err := profilePurgeTargetFromConfig(name, cfg)
	if err != nil || !target.removalSlotsVerified {
		return false
	}
	promotion := target.pendingSlotPresent &&
		target.pendingSecureBaseURL == target.secureBaseURL &&
		target.pendingSpkiPin == target.spkiPin
	return !target.currentSlotPresent && target.secureBaseURL != "" && target.revokedCurrentID == "" && !promotion ||
		!target.pendingSlotPresent && target.pendingSecureBaseURL != "" && target.revokedPendingID == ""
}
