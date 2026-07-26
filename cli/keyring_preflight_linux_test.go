//go:build linux

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

type linuxKeyringProbeTestBackend struct {
	value       string
	found       bool
	setErr      error
	getErr      error
	deleteErr   error
	setCalls    int
	getCalls    int
	deleteCalls int
	policies    []SecretStoreUIPolicy
}

func (b *linuxKeyringProbeTestBackend) Set(
	_ context.Context,
	_, _, value string,
	ui SecretStoreUIPolicy,
) error {
	b.setCalls++
	b.policies = append(b.policies, ui)
	if b.setErr == nil {
		b.value, b.found = value, true
	}
	return b.setErr
}

func (b *linuxKeyringProbeTestBackend) Get(
	_ context.Context,
	_, _ string,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	b.getCalls++
	b.policies = append(b.policies, ui)
	return b.value, b.found, b.getErr
}

func (b *linuxKeyringProbeTestBackend) Delete(
	_ context.Context,
	_, _ string,
	ui SecretStoreUIPolicy,
) error {
	b.deleteCalls++
	b.policies = append(b.policies, ui)
	if b.deleteErr == nil {
		b.value, b.found = "", false
	}
	return b.deleteErr
}

func TestRelayAuthTokenPreflightSessionBusWithTimeoutReusesHungProbe(t *testing.T) {
	originalSessionBus := relayAuthTokenPreflightSessionBus
	defer func() {
		relayAuthTokenPreflightSessionBus = originalSessionBus
		resetRelayAuthTokenPreflightSessionBusStateForTest()
	}()
	resetRelayAuthTokenPreflightSessionBusStateForTest()

	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	var calls atomic.Int32
	relayAuthTokenPreflightSessionBus = func() (*dbus.Conn, error) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-unblock
		return nil, context.Canceled
	}

	if conn, err := relayAuthTokenPreflightSessionBusWithTimeout(time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || conn != nil {
		t.Fatalf("expected first probe to time out, got conn=%v err=%v", conn, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected first probe to start")
	}

	if conn, err := relayAuthTokenPreflightSessionBusWithTimeout(time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || conn != nil {
		t.Fatalf("expected second probe to time out, got conn=%v err=%v", conn, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected timeout retry to reuse the in-flight probe, got %d SessionBus calls", got)
	}

	close(unblock)
	waitForRelayAuthTokenPreflightSessionBusIdleForTest(t)
}

func TestProbeLinuxKeyringWritableRoundTripsAndCleansUp(t *testing.T) {
	originalBackend := newLinuxKeyringProbeBackend
	t.Cleanup(func() {
		newLinuxKeyringProbeBackend = originalBackend
	})

	backend := &linuxKeyringProbeTestBackend{}
	newLinuxKeyringProbeBackend = func() (OAuthSecretBackend, error) {
		return backend, nil
	}

	if err := probeLinuxKeyringWritable(); err != nil {
		t.Fatalf("probeLinuxKeyringWritable() error = %v", err)
	}
	if backend.setCalls != 1 || backend.getCalls != 1 || backend.deleteCalls != 1 {
		t.Fatalf(
			"expected one set/get/delete cycle, got set=%d get=%d delete=%d",
			backend.setCalls,
			backend.getCalls,
			backend.deleteCalls,
		)
	}
	if backend.found {
		t.Fatalf("temporary probe secret was not deleted")
	}
	for _, policy := range backend.policies {
		if policy != SecretStoreForbidUI {
			t.Fatalf("probe allowed native UI with policy %q", policy)
		}
	}
}

func TestProbeLinuxKeyringWritableNeverPromptsWhenCollectionRelocks(t *testing.T) {
	originalBackend := newLinuxKeyringProbeBackend
	t.Cleanup(func() {
		newLinuxKeyringProbeBackend = originalBackend
	})

	backend := &linuxKeyringProbeTestBackend{
		setErr: newCloudError(CloudErrSecretUIForbidden, "unlock Secret Service", nil),
	}
	newLinuxKeyringProbeBackend = func() (OAuthSecretBackend, error) {
		return backend, nil
	}

	err := probeLinuxKeyringWritable()
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("expected relock to fail fast as locked, got %v", err)
	}
	if backend.getCalls != 0 || backend.deleteCalls != 0 {
		t.Fatalf("failed write continued probing: get=%d delete=%d", backend.getCalls, backend.deleteCalls)
	}
}

func TestLinuxDeviceCredentialPreflightAllowsExplicitPrompt(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	deviceCredentialPromptSessionAvailable = func() bool { return true }
	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		t.Fatal("explicit prompt preflight ran the no-UI Secret Service inspection")
		return linuxSecureStorageState{}, nil
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		inspectLinuxSecureStorageStateForKeyring = originalInspect
	})

	if err := linuxDeviceCredentialPreflight(
		context.Background(),
		SecretStoreAllowUI,
	); err != nil {
		t.Fatalf("explicit prompt preflight error = %v", err)
	}
}

func TestLinuxDeviceCredentialPreflightRejectsUnsafePromptSession(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		t.Fatal("unsafe prompt session reached Secret Service inspection")
		return linuxSecureStorageState{}, nil
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		inspectLinuxSecureStorageStateForKeyring = originalInspect
	})

	err := linuxDeviceCredentialPreflight(
		context.Background(),
		SecretStoreAllowUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("unsafe prompt session error = %v", err)
	}
}

func TestLinuxDeviceCredentialPreflightForbidUIFailsLocked(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	deviceCredentialPromptSessionAvailable = func() bool {
		t.Fatal("no-UI preflight consulted interactive prompt state")
		return false
	}
	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateLocked}, nil
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		inspectLinuxSecureStorageStateForKeyring = originalInspect
	})

	err := linuxDeviceCredentialPreflight(
		context.Background(),
		SecretStoreForbidUI,
	)
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("no-UI locked preflight error = %v", err)
	}
}

func resetRelayAuthTokenPreflightSessionBusStateForTest() {
	relayAuthTokenPreflightSessionBusState.Lock()
	relayAuthTokenPreflightSessionBusState.current = nil
	relayAuthTokenPreflightSessionBusState.Unlock()
}

func waitForRelayAuthTokenPreflightSessionBusIdleForTest(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		relayAuthTokenPreflightSessionBusState.Lock()
		idle := relayAuthTokenPreflightSessionBusState.current == nil
		relayAuthTokenPreflightSessionBusState.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for SessionBus probe to finish")
}
