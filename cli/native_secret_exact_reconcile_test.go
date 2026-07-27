package main

import (
	"context"
	"errors"
	"testing"
)

func TestExactDeleteReconciliationPreservesReplacement(
	t *testing.T,
) {
	ambiguous := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"delete exact",
		errors.New("worker exited"),
	)
	for _, test := range []struct {
		name    string
		value   string
		found   bool
		readErr error
		code    CloudErrorCode
	}{
		{name: "confirmed absent"},
		{
			name:  "expected remains",
			value: "expected",
			found: true,
			code:  CloudErrSecretOutcomeUnknown,
		},
		{
			name:  "replacement remains",
			value: "replacement",
			found: true,
			code:  CloudErrSecretConflict,
		},
		{
			name:    "fresh read fails",
			value:   "unreadable",
			readErr: errors.New("read failed"),
			code:    CloudErrSecretOutcomeUnknown,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := []byte(test.value)
			err := reconcileSecretDeleteExactWithRead(
				context.Background(),
				[]byte("expected"),
				ambiguous,
				func(context.Context) ([]byte, bool, error) {
					return value, test.found, test.readErr
				},
			)
			if test.code == "" {
				if err != nil {
					t.Fatalf("reconcile error = %v", err)
				}
			} else if !IsCloudErrorCode(err, test.code) {
				t.Fatalf(
					"reconcile error = %v, want %s",
					err,
					test.code,
				)
			}
			for _, part := range value {
				if part != 0 {
					t.Fatal(
						"reconciliation left secret bytes in memory",
					)
				}
			}
		})
	}
}
