//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

func preserveLinuxRecoveryHooks(t *testing.T) {
	t.Helper()
	originalBus := secureStorageRecoverySessionBusWithTimeout
	originalInspect := inspectLinuxSecureStorageStateWithConnForRecovery
	originalCreate := createLinuxSecureStorageCollectionForRecovery
	originalUnlock := unlockLinuxSecureStorageCollectionForRecovery
	originalCollectionLocked := secureStorageRecoveryCollectionLocked
	originalInteractive := secureStorageRecoveryInteractiveSession
	originalTimeout := secureStorageRecoveryPromptTimeout
	t.Cleanup(func() {
		secureStorageRecoverySessionBusWithTimeout = originalBus
		inspectLinuxSecureStorageStateWithConnForRecovery = originalInspect
		createLinuxSecureStorageCollectionForRecovery = originalCreate
		unlockLinuxSecureStorageCollectionForRecovery = originalUnlock
		secureStorageRecoveryCollectionLocked = originalCollectionLocked
		secureStorageRecoveryInteractiveSession = originalInteractive
		secureStorageRecoveryPromptTimeout = originalTimeout
	})
}

func configureLinuxRecoveryTest(
	t *testing.T,
	state linuxSecureStorageState,
) *dbus.Conn {
	t.Helper()
	preserveLinuxRecoveryHooks(t)
	conn := &dbus.Conn{}
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return conn, nil
	}
	inspectLinuxSecureStorageStateWithConnForRecovery = func(got *dbus.Conn) (linuxSecureStorageState, error) {
		if got != conn {
			t.Fatal("recovery inspected a different Secret Service connection")
		}
		return state, nil
	}
	secureStorageRecoveryInteractiveSession = func() bool { return true }
	return conn
}

func TestDetectPlatformSecureStorageRecoverySupportUsesSecretServiceAPI(t *testing.T) {
	configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind:              linuxSecureStorageStateLocked,
		defaultCollection: "/org/freedesktop/secrets/collection/kdewallet",
	})

	supported, err := detectPlatformSecureStorageRecoverySupport()
	if err != nil {
		t.Fatalf("detectPlatformSecureStorageRecoverySupport() error = %v", err)
	}
	if !supported {
		t.Fatal("expected standards-based Secret Service recovery support")
	}
}

func TestDetectPlatformSecureStorageRecoverySupportFailsWithoutSessionBus(t *testing.T) {
	preserveLinuxRecoveryHooks(t)
	wantErr := desktopKeyringSessionUnavailableError("no desktop session bus")
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		return nil, wantErr
	}

	supported, err := detectPlatformSecureStorageRecoverySupport()
	if supported || !errors.Is(err, wantErr) {
		t.Fatalf("supported=%v err=%v, want fail-closed session error", supported, err)
	}
}

func TestInferPlatformSecureStorageRecoveryActionUsesLiveState(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForRecovery
	t.Cleanup(func() { inspectLinuxSecureStorageStateForRecovery = originalInspect })

	for _, test := range []struct {
		name  string
		state linuxSecureStorageStateKind
		want  platformSecureStorageRecoveryAction
	}{
		{"missing collection", linuxSecureStorageStateNeedsInit, platformSecureStorageRecoveryInitialize},
		{"locked collection", linuxSecureStorageStateLocked, platformSecureStorageRecoveryUnlock},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspectLinuxSecureStorageStateForRecovery = func() (linuxSecureStorageState, error) {
				return linuxSecureStorageState{kind: test.state}, nil
			}
			action, err := inferPlatformSecureStorageRecoveryAction(
				desktopKeyringSetupRequiredError("generic setup required"),
			)
			if err != nil || action != test.want {
				t.Fatalf("action=%q err=%v, want %q", action, err, test.want)
			}
		})
	}
}

func TestClassifyAmbiguousDesktopKeyringSetupErrorUsesLiveState(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForClassification
	t.Cleanup(func() {
		inspectLinuxSecureStorageStateForClassification = originalInspect
	})
	rawErr := errors.New("failed to unlock correct collection")

	inspectLinuxSecureStorageStateForClassification = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateNeedsInit}, nil
	}
	if err := classifyAmbiguousDesktopKeyringSetupError(rawErr); !isDesktopKeyringInitializationRequiredError(err) {
		t.Fatalf("expected initialization-required classification, got %v", err)
	}

	inspectLinuxSecureStorageStateForClassification = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateLocked}, nil
	}
	if err := classifyAmbiguousDesktopKeyringSetupError(rawErr); !isDesktopKeyringLockedError(err) {
		t.Fatalf("expected locked classification, got %v", err)
	}
}

func TestRunPlatformSecureStorageRecoveryCreatesWithNativePrompt(t *testing.T) {
	conn := configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind: linuxSecureStorageStateNeedsInit,
	})
	collection := dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	createCalls := 0
	createLinuxSecureStorageCollectionForRecovery = func(
		ctx context.Context,
		gotConn *dbus.Conn,
	) (dbus.ObjectPath, error) {
		createCalls++
		if gotConn != conn {
			t.Fatal("create used a different Secret Service connection")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("native setup prompt has no hard deadline")
		}
		return collection, nil
	}
	secureStorageRecoveryCollectionLocked = func(
		gotConn *dbus.Conn,
		gotCollection dbus.ObjectPath,
	) (bool, error) {
		if gotConn != conn || gotCollection != collection {
			t.Fatalf("unexpected created collection inspection: %q", gotCollection)
		}
		return false, nil
	}
	unlockLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
		dbus.ObjectPath,
		SecretStoreUIPolicy,
	) error {
		t.Fatal("collection setup must not launch a second native prompt")
		return nil
	}

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryInitialize); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", createCalls)
	}
}

func TestRunPlatformSecureStorageRecoveryUnlocksWithNativePrompt(t *testing.T) {
	collection := dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	conn := configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind:              linuxSecureStorageStateLocked,
		defaultCollection: collection,
	})
	unlockCalls := 0
	unlockLinuxSecureStorageCollectionForRecovery = func(
		_ context.Context,
		gotConn *dbus.Conn,
		gotCollection dbus.ObjectPath,
		ui SecretStoreUIPolicy,
	) error {
		unlockCalls++
		if gotConn != conn || gotCollection != collection || ui != SecretStoreAllowUI {
			t.Fatalf("unexpected native unlock args: collection=%q ui=%q", gotCollection, ui)
		}
		return nil
	}

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
	if unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", unlockCalls)
	}
}

func TestRunPlatformSecureStorageRecoveryNeverOpensSecondPromptAfterCreate(t *testing.T) {
	configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind: linuxSecureStorageStateNeedsInit,
	})
	collection := dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	createLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
	) (dbus.ObjectPath, error) {
		return collection, nil
	}
	secureStorageRecoveryCollectionLocked = func(
		*dbus.Conn,
		dbus.ObjectPath,
	) (bool, error) {
		return true, nil
	}
	unlockLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
		dbus.ObjectPath,
		SecretStoreUIPolicy,
	) error {
		t.Fatal("one setup action must never launch a second native prompt")
		return nil
	}

	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryInitialize)
	if err == nil || isDesktopKeyringSetupRequiredError(err) ||
		!strings.Contains(err.Error(), "rerun setup to unlock it") {
		t.Fatalf("expected non-retrying locked-new-collection error, got %v", err)
	}
}

func TestRunPlatformSecureStorageRecoveryFailsClosedHeadless(t *testing.T) {
	preserveLinuxRecoveryHooks(t)
	secureStorageRecoveryInteractiveSession = func() bool { return false }
	secureStorageRecoverySessionBusWithTimeout = func(time.Duration) (*dbus.Conn, error) {
		t.Fatal("headless recovery must not contact Secret Service")
		return nil, nil
	}

	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock)
	if !isDesktopKeyringSessionUnavailableError(err) {
		t.Fatalf("expected fail-closed desktop-session error, got %v", err)
	}
}

func TestNativeLinuxSecureStoragePromptAvailableRejectsHeadlessAndSSH(t *testing.T) {
	installSafeNativePromptContext(t)
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	t.Cleanup(func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	})
	writerSupportsTTYForSetup = func(io.Writer) bool {
		return true
	}
	uiInputSupportsTTY = func() bool { return true }

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	if !nativeLinuxSecureStoragePromptAvailable() {
		t.Fatal("expected a local Wayland terminal to allow the native prompt")
	}

	t.Setenv("SSH_CONNECTION", "192.0.2.1 12345 192.0.2.2 22")
	if nativeLinuxSecureStoragePromptAvailable() {
		t.Fatal("SSH session must not open a native prompt on another desktop")
	}

	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if nativeLinuxSecureStoragePromptAvailable() {
		t.Fatal("headless terminal must not attempt a native prompt")
	}

	t.Setenv("DISPLAY", "localhost:10.0")
	t.Setenv("XDG_SESSION_TYPE", "tty")
	if nativeLinuxSecureStoragePromptAvailable() {
		t.Fatal("forwarded display without a graphical login session must fail closed")
	}

	t.Setenv("XDG_SESSION_TYPE", "x11")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if nativeLinuxSecureStoragePromptAvailable() {
		t.Fatal("implicit D-Bus autolaunch must not target another desktop session")
	}
}

func TestLinuxNativePromptDetectsWSLEnvironment(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "")
	if !platformNativePromptRunsUnderWSL() {
		t.Fatal("WSL environment was not detected")
	}
}

func TestRunPlatformSecureStorageRecoveryRecomputesMissingCollection(t *testing.T) {
	configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind: linuxSecureStorageStateNeedsInit,
	})
	createLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
	) (dbus.ObjectPath, error) {
		t.Fatal("unlock action must not silently create a collection")
		return "", nil
	}

	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock)
	if !isDesktopKeyringInitializationRequiredError(err) {
		t.Fatalf("expected initialization-required error, got %v", err)
	}
}

func TestRunPlatformSecureStorageRecoveryPromptHasHardTimeout(t *testing.T) {
	configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind:              linuxSecureStorageStateLocked,
		defaultCollection: "/org/freedesktop/secrets/collection/login",
	})
	secureStorageRecoveryPromptTimeout = 10 * time.Millisecond
	unlockLinuxSecureStorageCollectionForRecovery = func(
		ctx context.Context,
		_ *dbus.Conn,
		_ dbus.ObjectPath,
		_ SecretStoreUIPolicy,
	) error {
		<-ctx.Done()
		return newCloudError(CloudErrTimeout, "wait for Secret Service prompt", ctx.Err())
	}

	started := time.Now()
	err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock)
	if !IsCloudErrorCode(err, CloudErrTimeout) {
		t.Fatalf("expected timeout identity, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("native prompt exceeded hard test timeout: %s", elapsed)
	}
}

func TestClassifyNativeSecureStorageLockedKeepsActionState(t *testing.T) {
	stillLocked := newCloudError(
		CloudErrSecretStoreLocked,
		"unlock Secret Service",
		nil,
	)
	initErr := classifyNativeSecureStorageRecoveryError(
		platformSecureStorageRecoveryInitialize,
		stillLocked,
	)
	if !isDesktopKeyringInitializationRequiredError(initErr) {
		t.Fatalf("create prompt dismissal lost initialization state: %v", initErr)
	}
	unlockErr := classifyNativeSecureStorageRecoveryError(
		platformSecureStorageRecoveryUnlock,
		stillLocked,
	)
	if !isDesktopKeyringLockedError(unlockErr) {
		t.Fatalf("unlock prompt dismissal lost locked state: %v", unlockErr)
	}
}

func TestClassifyNativeSecureStoragePromptDismissIsCanceled(t *testing.T) {
	promptDismissed := newCloudError(
		CloudErrSecretPromptCanceled,
		"complete Secret Service prompt",
		nil,
	)
	err := classifyNativeSecureStorageRecoveryError(
		platformSecureStorageRecoveryUnlock,
		promptDismissed,
	)
	if !errors.Is(err, errLocalSecureStoragePromptCanceled) {
		t.Fatalf("prompt dismissal lost cancellation identity: %v", err)
	}
}

func TestRunPlatformSecureStorageRecoveryDoesNotPromptWhenAlreadyWritable(t *testing.T) {
	configureLinuxRecoveryTest(t, linuxSecureStorageState{
		kind: linuxSecureStorageStateWritable,
	})
	createLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
	) (dbus.ObjectPath, error) {
		t.Fatal("writable storage must not create a collection")
		return "", nil
	}
	unlockLinuxSecureStorageCollectionForRecovery = func(
		context.Context,
		*dbus.Conn,
		dbus.ObjectPath,
		SecretStoreUIPolicy,
	) error {
		t.Fatal("writable storage must not open an unlock prompt")
		return nil
	}

	if err := runPlatformSecureStorageRecovery(platformSecureStorageRecoveryUnlock); err != nil {
		t.Fatalf("runPlatformSecureStorageRecovery() error = %v", err)
	}
}
