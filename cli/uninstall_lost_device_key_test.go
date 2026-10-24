package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lostKeyMenu answers the uninstall menu with "purge" and the lost-key
// question with finish.
type lostKeyMenu struct {
	finish string
	asked  int
}

func (m *lostKeyMenu) Run(_ io.Writer, spec setupMenuSpec) (string, error) {
	if spec.Title == "Finish removing HA NOVA from this computer?" {
		m.asked++
		if spec.DefaultValue != "cancel" {
			return "", errors.New("Enter must cancel the lost-key question")
		}
		return m.finish, nil
	}
	return "purge", nil
}

// lostKeyFixture: a paired install whose keyring was reset (rc1, Ubuntu):
// config.json names the Relay pairing, the device key is gone.
func lostKeyFixture(t *testing.T) (runtimePaths, *int) {
	t.Helper()
	paths := appRemovalPaths(t, appPathConfig)
	revokes := new(int)
	previous := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(string, string, string) error {
		*revokes++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = previous })
	stubAppRemoval(t, newFakeSupervisor(t), nil, func() (devicePeers, error) {
		return devicePeers{}, errors.New("this computer has no device pairing")
	})
	return paths, revokes
}

func stubLostKeyMenu(t *testing.T, finish string) *lostKeyMenu {
	t.Helper()
	stubUninstallMenu(t, "purge")
	menu := &lostKeyMenu{finish: finish}
	interactiveSetupMenuRunner = menu
	return menu
}

// The guided removal says the key is gone, names the NOVA page, and after a
// yes removes the rest, config.json included.
func TestGuidedUninstallFinishesWhenTheDeviceKeyIsLost(t *testing.T) {
	paths, revokes := lostKeyFixture(t)
	menu := stubLostKeyMenu(t, "finish")

	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 0 || menu.asked != 1 || *revokes != 0 {
		t.Fatalf("exit=%d asked=%d revokes=%d\n%s", exitCode, menu.asked, *revokes, output)
	}
	if fileExists(paths.ConfigFile) {
		t.Fatalf("config.json survived the finish:\n%s", output)
	}
	for _, want := range []string{lostDeviceKeyText, "click Revoke next to it"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "refusing cleanup") {
		t.Fatalf("technical refusal shown:\n%s", output)
	}
}

// Cancel (and Enter, the default) changes nothing.
func TestGuidedUninstallLostKeyCancelChangesNothing(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	menu := stubLostKeyMenu(t, "cancel")
	before := readTestFile(t, paths.ConfigFile)

	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 0 || menu.asked != 1 || !strings.Contains(output, "Uninstall cancelled") {
		t.Fatalf("exit=%d asked=%d\n%s", exitCode, menu.asked, output)
	}
	if after := readTestFile(t, paths.ConfigFile); after != before {
		t.Fatalf("config changed:\n%s\n---\n%s", before, after)
	}
}

// --yes can't ask: it stops before any change with one plain next step.
func TestYesPurgeWithLostDeviceKeyNamesOneNextStep(t *testing.T) {
	paths, revokes := lostKeyFixture(t)
	before := readTestFile(t, paths.ConfigFile)

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 || *revokes != 0 {
		t.Fatalf("exit=%d revokes=%d\n%s", exitCode, *revokes, output)
	}
	want := lostDeviceKeyText + " Next step: in a terminal on this computer, run ha-nova uninstall --purge and choose to finish removing."
	if !strings.Contains(output, want) || strings.Contains(output, "refusing cleanup") {
		t.Fatalf("output:\n%s", output)
	}
	if after := readTestFile(t, paths.ConfigFile); after != before {
		t.Fatalf("config changed:\n%s\n---\n%s", before, after)
	}
}

// A yes skips only the key it saw: one that comes back is still revoked, and
// a purge that stops leaves config.json as it was (no fallback transport).
func TestAcceptedLostKeyNeverWeakensTheRest(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	t.Cleanup(func() { acceptedLostDeviceKeys = nil })
	acceptedLostDeviceKeys = findLostDeviceKeys(paths)
	if acceptedLostDeviceKeys[defaultServerProfileName] != (lostDeviceKey{current: true, secureBaseURL: "https://ha.local:8792", spkiPin: "pin"}) {
		t.Fatalf("lost = %+v", acceptedLostDeviceKeys)
	}
	revokes := pairedForAppRemoval(t)
	if err := finalizeLocalUninstallWithProgressUnlocked(paths, installState{}, &uninstallReport{}, uninstallModePurge, nil, nil); err != nil {
		t.Fatal(err)
	}
	if *revokes != 1 {
		t.Fatalf("key that came back was not revoked: %d", *revokes)
	}

	paths, _ = lostKeyFixture(t)
	cfg := appPathConfig
	cfg.PendingSecureBaseURL = "https://ha.local:8792" // incomplete: stops the purge
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writePendingDeviceCredential(validCredential(181)); err != nil {
		t.Fatal(err)
	}
	before := readTestFile(t, paths.ConfigFile)
	stubLostKeyMenu(t, "finish")
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 1 || !strings.Contains(output, "incomplete") {
		t.Fatalf("exit=%d\n%s", exitCode, output)
	}
	if after := readTestFile(t, paths.ConfigFile); after != before {
		t.Fatalf("failed purge changed config:\n%s\n---\n%s", before, after)
	}
}

// An interrupted Windows purge reruns as recovery (no menu): it asks too,
// and the helper gets the yes for exactly the servers asked about.
func TestWindowsRecoveryAsksAboutALostKey(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	enableWindowsUninstallStatusChecks(t)
	status, err := beginWindowsUninstallStatus(paths, uninstallModePurge, installSourceBundle)
	if err != nil {
		t.Fatal(err)
	}
	_ = failWindowsUninstallStatus(paths, status, "token_cleanup", errors.New("interrupted"))
	menu := stubLostKeyMenu(t, "finish")
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, []string{"--purge"}) })
	if menu.asked != 1 || exitCode != 0 {
		t.Fatalf("exit=%d asked=%d\n%s", exitCode, menu.asked, output)
	}

	paths, _ = lostKeyFixture(t)
	t.Cleanup(func() { acceptedLostDeviceKeys = nil })
	acceptedLostDeviceKeys = findLostDeviceKeys(paths)
	args := strings.Join(windowsUninstallHelperArgs("h.exe", uninstallModePurge, false, nil), " ")
	value := lostDeviceKeyArg(acceptedLostDeviceKeys)
	if !strings.HasSuffix(args, "--accept-lost-keys "+value) || !strings.HasPrefix(value, "default=") {
		t.Fatalf("helper args = %s", args)
	}
	acceptedLostDeviceKeys = nil
	acceptLostDeviceKeysInHelper(paths, value+",cabin=x")
	if len(acceptedLostDeviceKeys) != 1 || !acceptedLostDeviceKeys[defaultServerProfileName].current {
		t.Fatalf("helper accepted %+v", acceptedLostDeviceKeys)
	}
	acceptedLostDeviceKeys = nil
	acceptLostDeviceKeysInHelper(paths, "default=0000") // a key the owner didn't see
	if len(acceptedLostDeviceKeys) != 0 {
		t.Fatalf("helper accepted a changed key: %+v", acceptedLostDeviceKeys)
	}
}

// No yes for a key that appeared after a removal checkpoint, a Link profile,
// or a file-storage install whose keyring may still hold the key.
func TestLostDeviceKeyFinishKeepsOtherStops(t *testing.T) {
	appeared := profilePurgeTarget{name: "cabin", removalCheckpointed: true}
	err := validateRequiredProfilePurgeSlots(appeared, profilePurgeSlotState{currentExists: true})
	if _, ok := lostDeviceKeyErrorText(err); ok || err == nil {
		t.Fatalf("appeared credential: %v", err)
	}
	link := profilePurgeTarget{name: "cabin", secureBaseURL: "https://ha.local:8792", spkiPin: "pin", cloudProfile: true}
	err = validateRequiredProfilePurgeSlots(link, profilePurgeSlotState{})
	if _, ok := lostDeviceKeyErrorText(err); ok || !strings.Contains(err.Error(), "current credential is missing") {
		t.Fatalf("Link profile: %v", err)
	}
	local := link
	local.cloudProfile = false
	err = validateRequiredProfilePurgeSlots(local, profilePurgeSlotState{keyMayExistElsewhere: true})
	if _, ok := lostDeviceKeyErrorText(err); ok || err == nil {
		t.Fatalf("other store may hold the key: %v", err)
	}
	// A yes never covers a key that stopped being askable.
	acceptedLostDeviceKeys = map[string]lostDeviceKey{"cabin": lostDeviceKeyOf(local, profilePurgeSlotState{})}
	t.Cleanup(func() { acceptedLostDeviceKeys = nil })
	if validateRequiredProfilePurgeSlots(local, profilePurgeSlotState{keyMayExistElsewhere: true}) == nil {
		t.Fatal("accepted key skipped the other-store stop")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A headless pairing interrupted after activation leaves its pending key in
// an unmarked file: that key is not "gone", so there is no finish for it.
func TestUnmarkedPendingFileIsNotALostKey(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	cfg := appPathConfig
	cfg.PendingSecureBaseURL, cfg.PendingSpkiPin = "https://ha.local:8792", "pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	pending := deviceCredentialPendingServiceForProfile(defaultServerProfileName)
	if err := deviceSecretFileSet(pending, validCredential(182)); err != nil {
		t.Fatal(err)
	}
	if deviceSecretFileBacked() {
		t.Fatal("fixture must stay keyring-backed")
	}
	if lost := findLostDeviceKeys(paths); len(lost) != 0 {
		t.Fatalf("unmarked pending file counted as lost: %+v", lost)
	}
}

// Codex #790: a half-written endpoint keeps the technical refusal. The
// finish is offered only when both endpoint fields of every lost slot are set.
func TestIncompleteEndpointIsNeverAnAskableLostKey(t *testing.T) {
	t.Cleanup(func() { acceptedLostDeviceKeys = nil })
	for name, target := range map[string]profilePurgeTarget{
		"current url only":  {name: "cabin", secureBaseURL: "https://cabin:18792"},
		"current pin only":  {name: "cabin", spkiPin: "PINB"},
		"pending url only":  {name: "cabin", pendingSecureBaseURL: "https://cabin:18792"},
		"pending pin only":  {name: "cabin", pendingSpkiPin: "PINB"},
		"complete + broken": {name: "cabin", secureBaseURL: "https://cabin:18792", spkiPin: "PINB", pendingSpkiPin: "PINB"},
	} {
		key := lostDeviceKeyOf(target, profilePurgeSlotState{})
		acceptedLostDeviceKeys = map[string]lostDeviceKey{"cabin": key}
		err := validateRequiredProfilePurgeSlots(target, profilePurgeSlotState{})
		if _, ok := lostDeviceKeyErrorText(err); ok || err == nil {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	// A removal checkpoint that fails its own validation gets no finish.
	corrupt := profilePurgeTarget{name: "cabin", secureBaseURL: "https://cabin:18792", spkiPin: "PINB", removalCheckpointed: true}
	acceptedLostDeviceKeys = map[string]lostDeviceKey{"cabin": lostDeviceKeyOf(corrupt, profilePurgeSlotState{})}
	if err := validateRequiredProfilePurgeSlots(corrupt, profilePurgeSlotState{}); err == nil {
		t.Fatal("unverified checkpoint: accepted")
	} else if _, ok := lostDeviceKeyErrorText(err); ok {
		t.Fatalf("unverified checkpoint askable: %v", err)
	}
	complete := profilePurgeTarget{name: "cabin", secureBaseURL: "https://cabin:18792", spkiPin: "PINB"}
	acceptedLostDeviceKeys = nil
	if _, ok := lostDeviceKeyErrorText(validateRequiredProfilePurgeSlots(complete, profilePurgeSlotState{})); !ok {
		t.Fatal("complete endpoints lost their finish")
	}
}

// Codex #790: a resumed removal reminds about the NOVA page unless the
// pending key it removes really revokes the same pairing (local, live,
// same endpoint) — the test lostDeviceKeyOf uses.
func TestServerRemovalResumeKeepsTheNovaPageStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	const url, pin = "https://cabin:18792", "PINB"
	pendingService := deviceCredentialPendingServiceForProfile("cabin")
	cfgWith := func(outcome serverRemovalCleanupOutcome) runtimeConfig {
		cfg := runtimeConfig{
			ProfileID: "p-cabin", RelaySecureBaseURL: url, RelaySpkiPin: pin,
			PendingSecureBaseURL: url, PendingSpkiPin: pin,
		}
		cfg.ServerRemoval = &serverRemovalCheckpoint{
			Schema: serverRemovalCheckpointSchema, ProfileID: "p-cabin",
			CurrentService: deviceCredentialServiceForProfile("cabin"), PendingService: pendingService,
			OriginalProfileSHA: "sha", PendingSlotPresent: true, ObservedPendingID: "seen",
			SecureBaseURL: url, SPKIPin: pin, PendingSecureURL: url, PendingSPKIPin: pin,
		}
		if outcome != "" {
			cfg.ServerRemoval.ProcessedPendingID, cfg.ServerRemoval.PendingOutcome = "seen", outcome
		}
		if err := validateServerRemovalCheckpoint("cabin", cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	for name, tc := range map[string]struct {
		slot    string
		outcome serverRemovalCleanupOutcome
		remind  bool
	}{
		"unprocessed local pending":    {slot: validCredential(41)},
		"unprocessed unreadable":       {slot: "not a credential", remind: true},
		"unprocessed slot gone":        {remind: true},
		"processed and revoked":        {outcome: serverRemovalCleanupRevoked},
		"processed, revoke failed":     {outcome: serverRemovalCleanupFailed, remind: true},
		"processed, nothing to revoke": {outcome: serverRemovalCleanupLocal, remind: true},
	} {
		_ = secretDelete(pendingService)
		if tc.slot != "" {
			if err := secretSet(pendingService, tc.slot); err != nil {
				t.Fatal(err)
			}
		}
		if got := serverRemovalLeftLostKey("cabin", cfgWith(tc.outcome)); got != tc.remind {
			t.Fatalf("%s: remind=%v, want %v", name, got, tc.remind)
		}
	}
}

// Local Codex review: a symlinked raw slot may still lead to the key, so it
// is not "gone" either.
func TestSymlinkedPendingSlotIsNotALostKey(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	cfg := appPathConfig
	cfg.PendingSecureBaseURL, cfg.PendingSpkiPin = "https://ha.local:8792", "pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	slot, err := deviceSecretFilePath(deviceCredentialPendingServiceForProfile(defaultServerProfileName))
	if err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(t.TempDir(), "held")
	if err := os.WriteFile(held, []byte(validCredential(183)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(slot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(held, slot); err != nil {
		t.Fatal(err)
	}
	if lost := findLostDeviceKeys(paths); len(lost) != 0 {
		t.Fatalf("symlinked pending slot counted as lost: %+v", lost)
	}
}

// Local Codex review: a checkpoint written for another profile generation
// proves nothing about the slots, so `uninstall --purge` keeps the stop.
func TestStaleCheckpointGenerationKeepsTheStop(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	previousConfirmation, previousHook := readServerRemoveConfirmationForCommand, serverRemovalPhaseHook
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand, serverRemovalPhaseHook = previousConfirmation, previousHook
	})
	readServerRemoveConfirmationForCommand = func(string) (string, error) { return "cabin", nil }
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "checkpoint-persisted" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if exit, output := captureCommandOutput(t, func() int { return runServerRemove(paths, []string{"cabin"}) }); exit != 1 {
		t.Fatalf("crash run exit=%d output=%s", exit, output)
	}
	raw := readTestFile(t, paths.ConfigFile)
	stale := regexp.MustCompile(`"original_profile_sha256":\s*"[^"]*"`).ReplaceAllString(raw, `"original_profile_sha256": "stale"`)
	if stale == raw {
		t.Fatal("fixture has no checkpoint generation")
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.name != "cabin" {
			continue
		}
		state, err := inspectProfilePurgeSlotState(target)
		if err != nil {
			t.Fatal(err)
		}
		if target.removalSlotsVerified || validateRequiredProfilePurgeSlots(target, state) == nil {
			t.Fatalf("stale checkpoint trusted: %+v", target)
		}
		return
	}
	t.Fatal("no cabin target")
}
