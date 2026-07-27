package main

import (
	"context"
	"testing"
)

func TestInteractiveCloudSetupContextWaitsForHumanInput(t *testing.T) {
	ctx, cancel := newInteractiveCloudSetupContext()

	if deadline, exists := ctx.Deadline(); exists {
		cancel()
		t.Fatalf("interactive Cloud setup has unexpected deadline %s", deadline)
	}
	holder, ok := ctx.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder)
	if !ok || holder == nil {
		cancel()
		t.Fatal("interactive Cloud setup context lacks a secret-access holder")
	}

	cancel()
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Fatalf("context error = %v, want context.Canceled", ctx.Err())
		}
	default:
		t.Fatal("cancel did not stop interactive Cloud setup context")
	}
}
