//go:build linux

package main

import (
	"context"
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
