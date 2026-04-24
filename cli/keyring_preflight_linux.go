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
	done     chan struct{}
	result   relayAuthTokenPreflightSessionBusResult
	waiters  int
	consumed bool
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
	shouldClose := call.waiters == 0 && conn != nil
	relayAuthTokenPreflightSessionBusState.Unlock()

	if shouldClose {
		_ = conn.Close()
	}
}

func finishRelayAuthTokenPreflightSessionBusCall(call *relayAuthTokenPreflightSessionBusCall) (*dbus.Conn, error) {
	relayAuthTokenPreflightSessionBusState.Lock()
	outcome := call.result
	call.waiters--
	if outcome.conn != nil {
		if call.consumed {
			relayAuthTokenPreflightSessionBusState.Unlock()
			return nil, context.DeadlineExceeded
		}
		call.consumed = true
	}
	relayAuthTokenPreflightSessionBusState.Unlock()
	return outcome.conn, outcome.err
}

func releaseRelayAuthTokenPreflightSessionBusCall(call *relayAuthTokenPreflightSessionBusCall) {
	relayAuthTokenPreflightSessionBusState.Lock()
	if call.waiters > 0 {
		call.waiters--
	}
	done := false
	select {
	case <-call.done:
		done = true
	default:
	}
	var conn *dbus.Conn
	if done && call.waiters == 0 && !call.consumed && call.result.conn != nil {
		conn = call.result.conn
		call.result.conn = nil
	}
	relayAuthTokenPreflightSessionBusState.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
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
