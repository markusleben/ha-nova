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
