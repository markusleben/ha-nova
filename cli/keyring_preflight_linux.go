//go:build linux

package main

import (
	"context"
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

func relayAuthTokenPreflightSessionBusWithTimeout(timeout time.Duration) (*dbus.Conn, error) {
	type result struct {
		conn *dbus.Conn
		err  error
	}

	done := make(chan result, 1)
	go func() {
		conn, err := relayAuthTokenPreflightSessionBus()
		done <- result{conn: conn, err: err}
	}()

	select {
	case outcome := <-done:
		return outcome.conn, outcome.err
	case <-time.After(timeout):
		return nil, context.DeadlineExceeded
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
