//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

func TestLinuxOAuthSetLostReplyReconcilesCommittedAndUncommittedOutcomes(
	t *testing.T,
) {
	originalRead := linuxOAuthSecretReadForReconciliation
	t.Cleanup(func() {
		linuxOAuthSecretReadForReconciliation = originalRead
	})
	backend := &linuxOAuthSecretBackend{}
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"write OAuth secret",
		errors.New("lost D-Bus reply"),
	)
	for _, test := range []struct {
		name     string
		value    []byte
		found    bool
		readErr  error
		deadline bool
		ok       bool
		code     CloudErrorCode
	}{
		{
			name:     "committed",
			value:    []byte("expected"),
			found:    true,
			deadline: true,
			ok:       true,
		},
		{
			name:  "uncommitted",
			found: false,
			code:  CloudErrSecretOutcomeUnknown,
		},
		{
			name:     "conflicting value",
			value:    []byte("different"),
			found:    true,
			deadline: true,
			code:     CloudErrSecretConflict,
		},
		{
			name:    "read failure",
			value:   []byte("unreadable"),
			readErr: errors.New("fresh read failed"),
			code:    CloudErrSecretOutcomeUnknown,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, holder := expiredSecretReconciliationParent(
				t,
				test.deadline,
			)
			linuxOAuthSecretReadForReconciliation = func(
				readCtx context.Context,
				gotBackend *linuxOAuthSecretBackend,
				service, account string,
			) ([]byte, bool, error) {
				assertFreshSecretReconciliationContext(
					t,
					readCtx,
					holder,
				)
				if gotBackend != backend ||
					service != oauthSecretPendingService ||
					account != "profile-test" {
					t.Fatalf(
						"reconciliation target = %p/%q/%q",
						gotBackend,
						service,
						account,
					)
				}
				return append([]byte(nil), test.value...),
					test.found,
					test.readErr
			}
			err := reconcileLinuxOAuthSecretSet(
				parent,
				backend,
				oauthSecretPendingService,
				"profile-test",
				[]byte("expected"),
				ambiguous,
			)
			if test.ok {
				if err != nil {
					t.Fatalf("reconcile Set error = %v", err)
				}
				return
			}
			if !IsCloudErrorCode(err, test.code) {
				t.Fatalf("reconcile Set error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestLinuxOAuthDeleteLostReplyReconcilesCommittedAndUncommittedOutcomes(
	t *testing.T,
) {
	originalRead := linuxOAuthSecretReadForReconciliation
	t.Cleanup(func() {
		linuxOAuthSecretReadForReconciliation = originalRead
	})
	backend := &linuxOAuthSecretBackend{}
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"delete OAuth secret",
		errors.New("lost D-Bus reply"),
	)
	for _, test := range []struct {
		name     string
		found    bool
		readErr  error
		deadline bool
		ok       bool
	}{
		{name: "committed", deadline: true, ok: true},
		{name: "uncommitted", found: true},
		{
			name:     "read failure",
			readErr:  errors.New("fresh read failed"),
			deadline: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, holder := expiredSecretReconciliationParent(
				t,
				test.deadline,
			)
			linuxOAuthSecretReadForReconciliation = func(
				readCtx context.Context,
				gotBackend *linuxOAuthSecretBackend,
				service, account string,
			) ([]byte, bool, error) {
				assertFreshSecretReconciliationContext(
					t,
					readCtx,
					holder,
				)
				if gotBackend != backend ||
					service != oauthSecretCurrentService ||
					account != "profile-test" {
					t.Fatalf(
						"reconciliation target = %p/%q/%q",
						gotBackend,
						service,
						account,
					)
				}
				if test.found {
					return []byte("still-present"), true, test.readErr
				}
				return []byte("read-value"), false, test.readErr
			}
			err := reconcileLinuxOAuthSecretDeleteExpected(
				parent,
				backend,
				oauthSecretCurrentService,
				"profile-test",
				nil,
				ambiguous,
			)
			if test.ok {
				if err != nil {
					t.Fatalf("reconcile Delete error = %v", err)
				}
				return
			}
			if !IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) {
				t.Fatalf("reconcile Delete error = %v", err)
			}
		})
	}
}

func TestLinuxSecretServiceForbidUIReturnsBeforePrompt(t *testing.T) {
	_, err := linuxSecretServiceHandlePrompt(
		context.Background(),
		nil,
		dbus.ObjectPath("/org/freedesktop/secrets/prompt/p1"),
		SecretStoreForbidUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("forbid-ui prompt error = %v", err)
	}
}

func TestLinuxSecretServicePromptBudgetAllowsAtMostOnePrompt(t *testing.T) {
	ctx := linuxSecretServiceSinglePromptContext(
		context.Background(),
		SecretStoreAllowUI,
	)
	if err := linuxSecretServiceConsumePromptBudget(
		ctx,
		SecretStoreAllowUI,
	); err != nil {
		t.Fatalf("first prompt budget use failed: %v", err)
	}
	err := linuxSecretServiceConsumePromptBudget(ctx, SecretStoreAllowUI)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("second prompt budget use error = %v", err)
	}
}

func TestLinuxSecretServiceConnectionHonorsCallerDeadline(t *testing.T) {
	originalSessionBus := relayAuthTokenPreflightSessionBus
	resetRelayAuthTokenPreflightSessionBusStateForTest()
	unblock := make(chan struct{})
	relayAuthTokenPreflightSessionBus = func() (*dbus.Conn, error) {
		<-unblock
		return nil, context.Canceled
	}
	t.Cleanup(func() {
		close(unblock)
		waitForRelayAuthTokenPreflightSessionBusIdleForTest(t)
		relayAuthTokenPreflightSessionBus = originalSessionBus
		resetRelayAuthTokenPreflightSessionBusStateForTest()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err := linuxOAuthSecretCollection(ctx, SecretStoreForbidUI)
	if !IsCloudErrorCode(err, CloudErrTimeout) {
		t.Fatalf("linuxOAuthSecretCollection() error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Secret Service connection exceeded caller deadline: %s", elapsed)
	}
}

func TestLinuxOAuthBackendRejectsAllowUIWithoutInteractiveDesktop(t *testing.T) {
	originalInputTTY := uiInputSupportsTTY
	originalOutputTTY := writerSupportsTTYForSetup
	uiInputSupportsTTY = func() bool { return false }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	t.Cleanup(func() {
		uiInputSupportsTTY = originalInputTTY
		writerSupportsTTYForSetup = originalOutputTTY
	})

	backend := &linuxOAuthSecretBackend{}
	err := backend.Set(
		context.Background(),
		oauthSecretCurrentService,
		"default",
		"secret",
		SecretStoreAllowUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("headless AllowUI error = %v", err)
	}
}
