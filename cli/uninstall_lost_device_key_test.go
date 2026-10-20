package main

import (
	"errors"
	"io"
	"os"
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
	for _, want := range []string{lostDeviceKeyText, lostDeviceKeyHandStep} {
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
	want := lostDeviceKeyText + " Next step: run ha-nova uninstall (without --yes) and pick Remove."
	if !strings.Contains(output, want) || strings.Contains(output, "refusing cleanup") {
		t.Fatalf("output:\n%s", output)
	}
	if after := readTestFile(t, paths.ConfigFile); after != before {
		t.Fatalf("config changed:\n%s\n---\n%s", before, after)
	}
}

// The yes forgets only a key that is still lost: one that came back keeps
// its endpoint, so the purge still revokes it.
func TestForgetLostDeviceKeysSkipsAKeyThatCameBack(t *testing.T) {
	paths, _ := lostKeyFixture(t)
	lost, ok := findLostDeviceKeys(paths)
	if !ok || lost[defaultServerProfileName] != (lostDeviceKeySlots{current: true}) {
		t.Fatalf("lost=%v ok=%v", lost, ok)
	}
	pairedForAppRemoval(t)
	if err := forgetLostDeviceKeys(paths, lost); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || cfg.RelaySecureBaseURL == "" || cfg.RelaySpkiPin == "" {
		t.Fatalf("endpoint of a present key was dropped: %+v err=%v", cfg, err)
	}
}

// Keys that appeared after a removal checkpoint, and Link profiles, keep the
// old stop: no question offered.
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
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
