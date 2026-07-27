package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type secretReconciliationUnrelatedContextKey struct{}

func TestReconcileNativeSecretSetRequiresExactCommittedValue(t *testing.T) {
	request := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretSet,
		UI:            SecretStoreForbidUI,
		Service:       oauthSecretCurrentService,
		Account:       "default",
		Value:         []byte("expected"),
	}
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"write",
		errors.New("worker exited"),
	)
	for _, test := range []struct {
		name     string
		response nativeSecretWorkerResponse
		readErr  error
		deadline bool
		code     CloudErrorCode
	}{
		{
			name: "exact",
			response: nativeSecretWorkerResponse{
				SchemaVersion: nativeSecretWorkerSchema,
				Found:         true,
				Value:         []byte("expected"),
			},
			deadline: true,
		},
		{
			name: "missing",
			response: nativeSecretWorkerResponse{
				SchemaVersion: nativeSecretWorkerSchema,
			},
			code: CloudErrSecretOutcomeUnknown,
		},
		{
			name: "different",
			response: nativeSecretWorkerResponse{
				SchemaVersion: nativeSecretWorkerSchema,
				Found:         true,
				Value:         []byte("different"),
			},
			deadline: true,
			code:     CloudErrSecretConflict,
		},
		{
			name: "read failure",
			response: nativeSecretWorkerResponse{
				SchemaVersion: nativeSecretWorkerSchema,
				Value:         []byte("must-zero"),
			},
			readErr: errors.New("read failed"),
			code:    CloudErrSecretOutcomeUnknown,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, holder := expiredSecretReconciliationParent(
				t,
				test.deadline,
			)
			original := nativeSecretWorkerProcessForReconciliation
			nativeSecretWorkerProcessForReconciliation = func(
				readCtx context.Context,
				readRequest nativeSecretWorkerRequest,
			) (nativeSecretWorkerResponse, error) {
				assertFreshSecretReconciliationContext(
					t,
					readCtx,
					holder,
				)
				if readRequest.Operation != nativeSecretGet ||
					readRequest.UI != SecretStoreForbidUI ||
					readRequest.Service != request.Service ||
					readRequest.Account != request.Account {
					t.Fatalf("reconciliation read = %+v", readRequest)
				}
				return test.response, test.readErr
			}
			t.Cleanup(func() {
				nativeSecretWorkerProcessForReconciliation = original
			})
			err := reconcileNativeSecretSet(
				parent,
				request,
				ambiguous,
			)
			if test.code == "" {
				if err != nil {
					t.Fatalf("reconcile Set error = %v", err)
				}
			} else if !IsCloudErrorCode(err, test.code) {
				t.Fatalf("reconcile Set error = %v", err)
			}
			if test.readErr != nil {
				for _, value := range test.response.Value {
					if value != 0 {
						t.Fatal("failed reconciliation read left secret bytes in memory")
					}
				}
			}
		})
	}
}

func TestReconcileNativeSecretDeleteAcceptsOnlyConfirmedAbsence(
	t *testing.T,
) {
	request := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretDelete,
		UI:            SecretStoreForbidUI,
		Service:       oauthSecretCurrentService,
		Account:       "default",
	}
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"delete",
		errors.New("worker exited"),
	)
	for _, found := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[found], func(t *testing.T) {
			parent, holder := expiredSecretReconciliationParent(t, found)
			original := nativeSecretWorkerProcessForReconciliation
			nativeSecretWorkerProcessForReconciliation = func(
				readCtx context.Context,
				_ nativeSecretWorkerRequest,
			) (nativeSecretWorkerResponse, error) {
				assertFreshSecretReconciliationContext(
					t,
					readCtx,
					holder,
				)
				response := nativeSecretWorkerResponse{
					SchemaVersion: nativeSecretWorkerSchema,
					Found:         found,
				}
				if found {
					response.Value = []byte("still-present")
				}
				return response, nil
			}
			t.Cleanup(func() {
				nativeSecretWorkerProcessForReconciliation = original
			})
			err := reconcileNativeSecretDelete(
				parent,
				request,
				ambiguous,
			)
			if found && !IsCloudErrorCode(
				err,
				CloudErrSecretOutcomeUnknown,
			) {
				t.Fatalf("present Delete error = %v", err)
			}
			if !found && err != nil {
				t.Fatalf("absent Delete error = %v", err)
			}
		})
	}
}

func TestReconcileNativeSecretDeleteKeepsAmbiguityWhenFreshReadFails(
	t *testing.T,
) {
	parent, holder := expiredSecretReconciliationParent(t, true)
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"delete",
		errors.New("worker deadline"),
	)
	secret := []byte("must-zero")
	original := nativeSecretWorkerProcessForReconciliation
	nativeSecretWorkerProcessForReconciliation = func(
		readCtx context.Context,
		_ nativeSecretWorkerRequest,
	) (nativeSecretWorkerResponse, error) {
		assertFreshSecretReconciliationContext(t, readCtx, holder)
		return nativeSecretWorkerResponse{Value: secret},
			errors.New("fresh read failed")
	}
	t.Cleanup(func() {
		nativeSecretWorkerProcessForReconciliation = original
	})

	err := reconcileNativeSecretDelete(
		parent,
		nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretDelete,
			UI:            SecretStoreForbidUI,
			Service:       oauthSecretCurrentService,
			Account:       "default",
		},
		ambiguous,
	)
	if !IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) {
		t.Fatalf("reconcile Delete error = %v", err)
	}
	for _, value := range secret {
		if value != 0 {
			t.Fatal("failed reconciliation read left secret bytes in memory")
		}
	}
}

func expiredSecretReconciliationParent(
	t *testing.T,
	deadline bool,
) (context.Context, *cloudSecretAccessHolder) {
	t.Helper()
	base := context.WithValue(
		context.Background(),
		secretReconciliationUnrelatedContextKey{},
		"must-not-cross",
	)
	base = withCloudSecretAccessHolder(base)
	holder, _ := base.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder)
	if holder == nil {
		t.Fatal("secret-access holder was not installed")
	}
	base = context.WithValue(
		base,
		cloudSecretAccessContextKey{},
		&cloudSecretAccessSession{profileID: "must-not-cross"},
	)
	if deadline {
		ctx, cancel := context.WithDeadline(
			base,
			time.Now().Add(-time.Second),
		)
		t.Cleanup(cancel)
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("parent error = %v, want deadline", ctx.Err())
		}
		return ctx, holder
	}
	ctx, cancel := context.WithCancel(base)
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("parent error = %v, want canceled", ctx.Err())
	}
	return ctx, holder
}

func assertFreshSecretReconciliationContext(
	t *testing.T,
	ctx context.Context,
	holder *cloudSecretAccessHolder,
) {
	t.Helper()
	if err := ctx.Err(); err != nil {
		t.Fatalf("reconciliation inherited caller cancellation: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 ||
		time.Until(deadline) > nativeOAuthSecretNoUITimeout {
		t.Fatalf("reconciliation deadline = %v, exists=%v", deadline, ok)
	}
	if got, _ := ctx.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder); got != holder {
		t.Fatal("reconciliation lost the secret-access holder")
	}
	if got := ctx.Value(secretReconciliationUnrelatedContextKey{}); got != nil {
		t.Fatalf("reconciliation copied unrelated caller value %v", got)
	}
	if got := ctx.Value(cloudSecretAccessContextKey{}); got != nil {
		t.Fatal("reconciliation copied the direct secret-access session")
	}
}
