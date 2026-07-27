//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

const (
	secretServiceDBusName      = "org.freedesktop.secrets"
	secretServiceDBusPath      = "/org/freedesktop/secrets"
	secretServiceDBusInterface = "org.freedesktop.Secret.Service"
)

var relayAuthTokenPreflightSessionBus = dbus.SessionBus
var relayAuthTokenPreflightTimeout = 2 * time.Second
var newLinuxKeyringProbeBackend = newNativeCredentialSecretBackend

type relayAuthTokenPreflightSessionBusResult struct {
	conn *dbus.Conn
	err  error
}

type relayAuthTokenPreflightSessionBusCall struct {
	done    chan struct{}
	result  relayAuthTokenPreflightSessionBusResult
	waiters int
}

var relayAuthTokenPreflightSessionBusState struct {
	sync.Mutex
	current *relayAuthTokenPreflightSessionBusCall
}

func relayAuthTokenPreflightSessionBusWithTimeout(timeout time.Duration) (*dbus.Conn, error) {
	if timeout <= 0 {
		timeout = relayAuthTokenPreflightTimeout
	}
	call := beginRelayAuthTokenPreflightSessionBusCall()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-call.done:
		return finishRelayAuthTokenPreflightSessionBusCall(call)
	case <-timer.C:
		releaseRelayAuthTokenPreflightSessionBusCall(call)
		return nil, context.DeadlineExceeded
	}
}

func beginRelayAuthTokenPreflightSessionBusCall() *relayAuthTokenPreflightSessionBusCall {
	relayAuthTokenPreflightSessionBusState.Lock()
	call := relayAuthTokenPreflightSessionBusState.current
	start := false
	if call == nil {
		call = &relayAuthTokenPreflightSessionBusCall{done: make(chan struct{})}
		relayAuthTokenPreflightSessionBusState.current = call
		start = true
	}
	call.waiters++
	relayAuthTokenPreflightSessionBusState.Unlock()

	if start {
		go runRelayAuthTokenPreflightSessionBusCall(call)
	}
	return call
}

func runRelayAuthTokenPreflightSessionBusCall(call *relayAuthTokenPreflightSessionBusCall) {
	conn, err := relayAuthTokenPreflightSessionBus()

	relayAuthTokenPreflightSessionBusState.Lock()
	call.result = relayAuthTokenPreflightSessionBusResult{conn: conn, err: err}
	if relayAuthTokenPreflightSessionBusState.current == call {
		relayAuthTokenPreflightSessionBusState.current = nil
	}
	close(call.done)
	relayAuthTokenPreflightSessionBusState.Unlock()
}

func finishRelayAuthTokenPreflightSessionBusCall(call *relayAuthTokenPreflightSessionBusCall) (*dbus.Conn, error) {
	relayAuthTokenPreflightSessionBusState.Lock()
	outcome := call.result
	call.waiters--
	relayAuthTokenPreflightSessionBusState.Unlock()
	return outcome.conn, outcome.err
}

func releaseRelayAuthTokenPreflightSessionBusCall(call *relayAuthTokenPreflightSessionBusCall) {
	relayAuthTokenPreflightSessionBusState.Lock()
	if call.waiters > 0 {
		call.waiters--
	}
	relayAuthTokenPreflightSessionBusState.Unlock()
}

func relayAuthTokenPlatformSetupPreflightImpl() error {
	state, err := inspectLinuxSecureStorageState()
	if err != nil {
		return err
	}

	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
	case linuxSecureStorageStateLocked:
		return desktopKeyringLockedError("default Secret Service collection is locked")
	default:
		return normalizeLinuxKeyringError(probeLinuxKeyringWritable())
	}
}

func probeLinuxKeyringWritable() error {
	serviceSuffix := make([]byte, 8)
	if _, err := rand.Read(serviceSuffix); err != nil {
		return fmt.Errorf("cannot verify local secure storage: %w", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("cannot verify local secure storage: %w", err)
	}
	service := fmt.Sprintf(
		"%s.recovery-probe.%s",
		relayAuthTokenServiceName(),
		hex.EncodeToString(serviceSuffix),
	)
	probeSecret := hex.EncodeToString(secret)
	account, err := currentKeyringUsername()
	if err != nil {
		return err
	}
	backend, err := newLinuxKeyringProbeBackend()
	if err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	defer cancel()

	if err := backend.Set(ctx, service, account, probeSecret, SecretStoreForbidUI); err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	readBack, found, readErr := backend.Get(
		ctx,
		service,
		account,
		SecretStoreForbidUI,
	)
	if readErr != nil || !found || readBack != probeSecret {
		deleteErr := backend.Delete(ctx, service, account, SecretStoreForbidUI)
		if readErr != nil {
			return normalizeLinuxKeyringProbeError(errors.Join(readErr, deleteErr))
		}
		if deleteErr != nil {
			return normalizeLinuxKeyringProbeError(deleteErr)
		}
		if !found {
			return fmt.Errorf("cannot verify local secure storage: saved verification secret was not found")
		}
		return fmt.Errorf("cannot verify local secure storage: saved verification secret did not match")
	}
	if err := backend.Delete(ctx, service, account, SecretStoreForbidUI); err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	return nil
}

func normalizeLinuxKeyringProbeError(err error) error {
	switch {
	case err == nil:
		return nil
	case isDesktopKeyringSessionUnavailableError(err):
		return desktopKeyringSessionUnavailableError("local secure storage is unavailable in this Linux session")
	case IsCloudErrorCode(err, CloudErrSecretStoreLocked),
		IsCloudErrorCode(err, CloudErrSecretUIForbidden):
		return desktopKeyringLockedError("default Secret Service collection is locked")
	case IsCloudErrorCode(err, CloudErrTimeout):
		return desktopKeyringUnavailableError("Secret Service preflight timed out")
	case IsCloudErrorCode(err, CloudErrSecretStore):
		return desktopKeyringUnavailableError("Secret Service preflight failed")
	default:
		return localSecureStorageRecoveryError(err)
	}
}
