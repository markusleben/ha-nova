//go:build linux

package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

type fakeFileInfo struct {
	sys any
}

func (fakeFileInfo) Name() string       { return "gnome-keyring-daemon" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any         { return f.sys }

func TestDetectPlatformSecureStorageRecoverySupportRequiresTrustedOwner(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{
			comm:    "gnome-keyring-daemon",
			exePath: "/usr/bin/gnome-keyring-daemon",
		}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}

	supported, err := detectPlatformSecureStorageRecoverySupport()
	if err != nil {
		t.Fatalf("detectPlatformSecureStorageRecoverySupport() error = %v", err)
	}
	if !supported {
		t.Fatal("expected GNOME Keyring recovery support for a trusted root-owned daemon")
	}
}

func TestDetectPlatformSecureStorageRecoverySupportRejectsUntrustedOwner(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{
			comm:    "gnome-keyring-daemon",
			exePath: "/tmp/gnome-keyring-daemon",
		}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 1000}}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		t.Fatal("did not expect method check for untrusted owner")
		return false, nil
	}

	supported, err := detectPlatformSecureStorageRecoverySupport()
	if err != nil {
		t.Fatalf("detectPlatformSecureStorageRecoverySupport() error = %v", err)
	}
	if supported {
		t.Fatal("expected recovery support to reject non-root-owned executables")
	}
}

func TestDetectPlatformSecureStorageRecoverySupportRequiresGNOMEMethods(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return false, nil
	}

	supported, err := detectPlatformSecureStorageRecoverySupport()
	if err != nil {
		t.Fatalf("detectPlatformSecureStorageRecoverySupport() error = %v", err)
	}
	if supported {
		t.Fatal("expected recovery support to stay disabled when GNOME recovery methods are unavailable")
	}
}

func TestInferPlatformSecureStorageRecoveryActionUsesLiveStateForAmbiguousError(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForRecovery
	defer func() {
		inspectLinuxSecureStorageStateForRecovery = originalInspect
	}()

	inspectLinuxSecureStorageStateForRecovery = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateNeedsInit}, nil
	}
	action, err := inferPlatformSecureStorageRecoveryAction(desktopKeyringSetupRequiredError("generic setup required"))
	if err != nil {
		t.Fatalf("inferPlatformSecureStorageRecoveryAction() error = %v", err)
	}
	if action != platformSecureStorageRecoveryInitialize {
		t.Fatalf("expected initialize action, got %q", action)
	}

	inspectLinuxSecureStorageStateForRecovery = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateLocked}, nil
	}
	action, err = inferPlatformSecureStorageRecoveryAction(desktopKeyringSetupRequiredError("generic setup required"))
	if err != nil {
		t.Fatalf("inferPlatformSecureStorageRecoveryAction() error = %v", err)
	}
	if action != platformSecureStorageRecoveryUnlock {
		t.Fatalf("expected unlock action, got %q", action)
	}
}

func TestClassifyAmbiguousDesktopKeyringSetupErrorUsesLiveState(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForClassification
	defer func() {
		inspectLinuxSecureStorageStateForClassification = originalInspect
	}()

	rawErr := errors.New("failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'")

	inspectLinuxSecureStorageStateForClassification = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateNeedsInit}, nil
	}
	classified := classifyAmbiguousDesktopKeyringSetupError(rawErr)
	if !isDesktopKeyringInitializationRequiredError(classified) {
		t.Fatalf("expected initialization-required classification, got %v", classified)
	}

	inspectLinuxSecureStorageStateForClassification = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateLocked}, nil
	}
	classified = classifyAmbiguousDesktopKeyringSetupError(rawErr)
	if !isDesktopKeyringLockedError(classified) {
		t.Fatalf("expected locked classification, got %v", classified)
	}
}

func TestClassifyAmbiguousDesktopKeyringSetupErrorStopsWhenInspectionIsStillAmbiguous(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForClassification
	defer func() {
		inspectLinuxSecureStorageStateForClassification = originalInspect
	}()

	rawErr := errors.New("failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'")
	inspectLinuxSecureStorageStateForClassification = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{}, rawErr
	}

	if classified := classifyAmbiguousDesktopKeyringSetupError(rawErr); classified != nil {
		t.Fatalf("expected no classification when state inspection stays ambiguous, got %v", classified)
	}
}

func TestRunPlatformSecureStorageRecoveryInitializesWhenDefaultCollectionMissing(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	originalInitialize := initializeLinuxSecureStorageForRecovery
	originalProbe := secureStorageRecoveryProbe
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
		initializeLinuxSecureStorageForRecovery = originalInitialize
		secureStorageRecoveryProbe = originalProbe
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}

	var gotSecret string
	initializeLinuxSecureStorageForRecovery = func(_ *dbus.Conn, secret []byte) error {
		gotSecret = string(secret)
		return nil
	}
	secureStorageRecoveryProbe = func() error { return nil }

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryInitialize, []byte("linux-local-keyring")); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
	if gotSecret != "linux-local-keyring" {
		t.Fatalf("initialize secret = %q", gotSecret)
	}
}

func TestRunPlatformSecureStorageRecoveryUnlocksExistingCollection(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	originalInspect := inspectLinuxSecureStorageStateWithConnForRecovery
	originalUnlock := unlockLinuxSecureStorageForRecovery
	originalProbe := secureStorageRecoveryProbe
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
		inspectLinuxSecureStorageStateWithConnForRecovery = originalInspect
		unlockLinuxSecureStorageForRecovery = originalUnlock
		secureStorageRecoveryProbe = originalProbe
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}
	inspectLinuxSecureStorageStateWithConnForRecovery = func(*dbus.Conn) (linuxSecureStorageState, error) {
		return linuxSecureStorageState{
			kind:              linuxSecureStorageStateLocked,
			defaultCollection: dbus.ObjectPath("/org/freedesktop/secrets/collection/Login"),
		}, nil
	}

	var gotSecret string
	var gotCollection dbus.ObjectPath
	unlockLinuxSecureStorageForRecovery = func(_ *dbus.Conn, collection dbus.ObjectPath, secret []byte) error {
		gotCollection = collection
		gotSecret = string(secret)
		return nil
	}
	secureStorageRecoveryProbe = func() error { return nil }

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock, []byte("linux-local-keyring")); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
	if gotCollection != dbus.ObjectPath("/org/freedesktop/secrets/collection/Login") {
		t.Fatalf("unlock collection = %q", gotCollection)
	}
	if gotSecret != "linux-local-keyring" {
		t.Fatalf("unlock secret = %q", gotSecret)
	}
}

func TestRunPlatformSecureStorageRecoveryPreservesPasswordRejected(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	originalInspect := inspectLinuxSecureStorageStateWithConnForRecovery
	originalUnlock := unlockLinuxSecureStorageForRecovery
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
		inspectLinuxSecureStorageStateWithConnForRecovery = originalInspect
		unlockLinuxSecureStorageForRecovery = originalUnlock
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}
	inspectLinuxSecureStorageStateWithConnForRecovery = func(*dbus.Conn) (linuxSecureStorageState, error) {
		return linuxSecureStorageState{
			kind:              linuxSecureStorageStateLocked,
			defaultCollection: dbus.ObjectPath("/org/freedesktop/secrets/collection/Login"),
		}, nil
	}
	unlockLinuxSecureStorageForRecovery = func(*dbus.Conn, dbus.ObjectPath, []byte) error {
		return localSecureStoragePasswordRejectedError()
	}

	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock, []byte("linux-local-keyring"))
	if !errors.Is(err, errLocalSecureStoragePasswordRejected) {
		t.Fatalf("expected password rejection, got %v", err)
	}
}

func TestProbeLinuxKeyringWritableRoundTripsAndCleansUp(t *testing.T) {
	originalSet := keyringSetWithService
	originalGet := keyringGetWithService
	originalDelete := keyringDeleteWithService
	defer func() {
		keyringSetWithService = originalSet
		keyringGetWithService = originalGet
		keyringDeleteWithService = originalDelete
	}()

	storage := map[string]string{}
	setCalls := 0
	getCalls := 0
	deleteCalls := 0
	keyringSetWithService = func(service, username, secret string) error {
		setCalls++
		storage[service+"|"+username] = secret
		return nil
	}
	keyringGetWithService = func(service, username string) (string, error) {
		getCalls++
		value, ok := storage[service+"|"+username]
		if !ok {
			return "", errors.New("missing probe secret")
		}
		return value, nil
	}
	keyringDeleteWithService = func(service, username string) error {
		deleteCalls++
		delete(storage, service+"|"+username)
		return nil
	}

	if err := probeLinuxKeyringWritable(); err != nil {
		t.Fatalf("probeLinuxKeyringWritable() error = %v", err)
	}
	if setCalls != 1 || getCalls != 1 || deleteCalls != 1 {
		t.Fatalf("expected one set/get/delete probe cycle, got set=%d get=%d delete=%d", setCalls, getCalls, deleteCalls)
	}
	if len(storage) != 0 {
		t.Fatalf("expected probe cleanup to remove the temporary secret, remaining=%d", len(storage))
	}
}

func TestRunPlatformSecureStorageRecoveryFailsWhenPostInitProbeStillFails(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	originalInitialize := initializeLinuxSecureStorageForRecovery
	originalProbe := secureStorageRecoveryProbe
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
		initializeLinuxSecureStorageForRecovery = originalInitialize
		secureStorageRecoveryProbe = originalProbe
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}
	initializeLinuxSecureStorageForRecovery = func(*dbus.Conn, []byte) error { return nil }
	secureStorageRecoveryProbe = func() error {
		return localSecureStorageRecoveryError(desktopKeyringInitializationRequiredError("local secure storage still needs one-time setup on this Linux machine"))
	}

	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryInitialize, []byte("linux-local-keyring"))
	if !isDesktopKeyringInitializationRequiredError(err) {
		t.Fatalf("expected initialization-required probe failure, got %v", err)
	}
}

func TestRunPlatformSecureStorageRecoveryReprobesUnlockedCollection(t *testing.T) {
	originalOwner := secureStorageRecoveryOwnerProcess
	originalStat := secureStorageRecoveryStat
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalMethods := secureStorageRecoverySupportsGNOMEMethods
	originalInspect := inspectLinuxSecureStorageStateWithConnForRecovery
	originalUnlock := unlockLinuxSecureStorageForRecovery
	originalProbe := secureStorageRecoveryProbe
	defer func() {
		secureStorageRecoveryOwnerProcess = originalOwner
		secureStorageRecoveryStat = originalStat
		secureStorageRecoverySessionBusWithTimeout = originalBus
		secureStorageRecoverySupportsGNOMEMethods = originalMethods
		inspectLinuxSecureStorageStateWithConnForRecovery = originalInspect
		unlockLinuxSecureStorageForRecovery = originalUnlock
		secureStorageRecoveryProbe = originalProbe
	}()

	secureStorageRecoveryOwnerProcess = func() (secretServiceOwnerProcess, error) {
		return secretServiceOwnerProcess{comm: "gnome-keyring-daemon", exePath: "/usr/bin/gnome-keyring-daemon"}, nil
	}
	secureStorageRecoveryStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{sys: &syscall.Stat_t{Uid: 0}}, nil
	}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return &dbus.Conn{}, nil
	}
	secureStorageRecoverySupportsGNOMEMethods = func(*dbus.Conn) (bool, error) {
		return true, nil
	}
	inspectLinuxSecureStorageStateWithConnForRecovery = func(*dbus.Conn) (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateWritable}, nil
	}
	unlockLinuxSecureStorageForRecovery = func(*dbus.Conn, dbus.ObjectPath, []byte) error {
		t.Fatal("did not expect unlock when the collection is already writable")
		return nil
	}
	probeCalls := 0
	secureStorageRecoveryProbe = func() error {
		probeCalls++
		return nil
	}

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock, []byte("linux-local-keyring")); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("expected one post-recovery probe, got %d", probeCalls)
	}
}
