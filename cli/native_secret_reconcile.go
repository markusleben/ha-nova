package main

import (
	"context"
	"crypto/subtle"
	"errors"
)

var nativeSecretWorkerProcessForReconciliation = runNativeSecretWorkerProcess

type nativeSecretReconciliationRead func(
	context.Context,
) ([]byte, bool, error)

func reconcileNativeSecretSet(
	ctx context.Context,
	request nativeSecretWorkerRequest,
	mutationErr error,
) error {
	return reconcileSecretSetWithRead(
		ctx,
		request.Value,
		mutationErr,
		func(readCtx context.Context) ([]byte, bool, error) {
			response, readErr := nativeSecretWorkerProcessForReconciliation(
				readCtx,
				nativeSecretWorkerRequest{
					SchemaVersion: nativeSecretWorkerSchema,
					Operation:     nativeSecretGet,
					UI:            SecretStoreForbidUI,
					Service:       request.Service,
					Account:       request.Account,
				},
			)
			return response.Value, response.Found, readErr
		},
	)
}

func reconcileSecretSetWithRead(
	ctx context.Context,
	expected []byte,
	mutationErr error,
	read nativeSecretReconciliationRead,
) error {
	if !IsCloudErrorCode(mutationErr, CloudErrSecretOutcomeUnknown) ||
		read == nil {
		return mutationErr
	}
	readCtx, cancel := freshNativeSecretReconciliationContext(ctx)
	defer cancel()
	value, found, readErr := read(readCtx)
	if readErr != nil {
		zeroSecretBytes(value)
		return ambiguousNativeSecretMutation(mutationErr, readErr)
	}
	defer zeroSecretBytes(value)
	if !found {
		return mutationErr
	}
	if len(value) != len(expected) ||
		subtle.ConstantTimeCompare(value, expected) != 1 {
		return newCloudError(
			CloudErrSecretConflict,
			"reconcile native secure-storage write",
			mutationErr,
		)
	}
	return nil
}

func reconcileNativeSecretDelete(
	ctx context.Context,
	request nativeSecretWorkerRequest,
	mutationErr error,
) error {
	return reconcileSecretDeleteWithRead(
		ctx,
		mutationErr,
		func(readCtx context.Context) ([]byte, bool, error) {
			response, readErr := nativeSecretWorkerProcessForReconciliation(
				readCtx,
				nativeSecretWorkerRequest{
					SchemaVersion: nativeSecretWorkerSchema,
					Operation:     nativeSecretGet,
					UI:            SecretStoreForbidUI,
					Service:       request.Service,
					Account:       request.Account,
				},
			)
			return response.Value, response.Found, readErr
		},
	)
}

func reconcileSecretDeleteWithRead(
	ctx context.Context,
	mutationErr error,
	read nativeSecretReconciliationRead,
) error {
	if !IsCloudErrorCode(mutationErr, CloudErrSecretOutcomeUnknown) ||
		read == nil {
		return mutationErr
	}
	readCtx, cancel := freshNativeSecretReconciliationContext(ctx)
	defer cancel()
	value, found, readErr := read(readCtx)
	if readErr != nil {
		zeroSecretBytes(value)
		return ambiguousNativeSecretMutation(mutationErr, readErr)
	}
	zeroSecretBytes(value)
	if !found {
		return nil
	}
	return mutationErr
}

// A mutation may have committed immediately before its caller deadline fired.
// Reconciliation therefore gets its own bounded lifetime. Copy only the
// holder used by the already-authorized secret-access session: arbitrary
// caller values, cancellation, deadlines, and UI permission must not cross
// this recovery boundary.
func freshNativeSecretReconciliationContext(
	parent context.Context,
) (context.Context, context.CancelFunc) {
	base := context.Background()
	if holder, ok := parent.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder); ok && holder != nil {
		base = context.WithValue(
			base,
			cloudSecretAccessHolderContextKey{},
			holder,
		)
	}
	return boundedNativeOAuthSecretContext(
		base,
		SecretStoreForbidUI,
	)
}

func ambiguousNativeSecretMutation(mutationErr, readErr error) error {
	return newCloudError(
		CloudErrSecretOutcomeUnknown,
		"reconcile native secure-storage mutation",
		errors.Join(mutationErr, readErr),
	)
}
