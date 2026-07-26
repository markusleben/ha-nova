package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloudReconnectRollbackPreservesPendingReplacementDuringRevoke(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	ctx := context.Background()
	current, err := store.CreatePending(
		ctx,
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err = store.PromotePending(
		ctx,
		current.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	pendingInput := productionCloudTestEnvelope()
	pendingInput.Generation = strings.Repeat("b", 32)
	pendingInput.RefreshToken = "refresh-pending"
	pendingInput.RefreshTokenID = "refresh-pending-id"
	pendingInput.HAUserID = "user-new"
	pending, err := store.CreatePending(
		ctx,
		pendingInput,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(
		productionCloudTestOrigin,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentMetadata := cloudMetadataFromEnvelope(origin, current)
	pendingMetadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID: "profile-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCloudVerified,
			Current: &currentMetadata,
			Pending: &pendingMetadata,
		},
	}
	ctx, _ = withCloudSecretAccessSession(
		ctx,
		cfg.ProfileID,
		store,
	)
	replacement := pending
	replacement.RefreshToken = "replacement-refresh"
	previousRevoke := revokeAndVerifyCloudAuthorizationForCLI
	revokeAndVerifyCloudAuthorizationForCLI = func(
		context.Context,
		OAuthSecretEnvelope,
	) error {
		return store.write(
			context.Background(),
			oauthSecretPendingService,
			replacement,
			SecretStoreForbidUI,
		)
	}
	t.Cleanup(func() {
		revokeAndVerifyCloudAuthorizationForCLI = previousRevoke
	})
	err = rollbackCloudReconnectAfterUserConflict(
		ctx,
		&cfg,
		func(runtimeConfig) error { return nil },
	)
	if !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("rollback error = %v", err)
	}
	if cfg.Cloud.State != cloudStateRollingBack ||
		cfg.Cloud.Pending == nil {
		t.Fatalf("rollback checkpoint was cleared: %+v", cfg.Cloud)
	}
	assertPendingOAuthReplacement(
		t,
		store,
		replacement,
	)
}

func TestRetirePreviousPreservesReplacementDuringRemoteRevoke(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	oldCurrent := establishOAuthCurrent(
		t,
		store,
		"old-refresh",
	)
	next, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope(
			"new-refresh",
			"new-token",
			"user-1",
			"relay-1",
		),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromotePending(
		context.Background(),
		next.Generation,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	replacement := oldCurrent
	replacement.State = OAuthSecretRetiring
	replacement.RefreshToken = "replacement-refresh"
	ctx, _ := withCloudSecretAccessSession(
		context.Background(),
		"profile-1",
		store,
	)
	previousRevoke := revokeAndVerifyCloudAuthorizationForCLI
	revokeAndVerifyCloudAuthorizationForCLI = func(
		context.Context,
		OAuthSecretEnvelope,
	) error {
		return store.write(
			context.Background(),
			oauthSecretRetiringService,
			replacement,
			SecretStoreForbidUI,
		)
	}
	t.Cleanup(func() {
		revokeAndVerifyCloudAuthorizationForCLI = previousRevoke
	})
	err = (productionCloudSetupCoordinator{}).RetirePrevious(
		ctx,
		"profile-1",
	)
	if !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("retire error = %v", err)
	}
	actual, exists, err := store.LoadRetiring(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists ||
		!sameOAuthSecretEnvelope(actual, replacement) {
		t.Fatalf(
			"replacement exists=%v value=%+v err=%v",
			exists,
			actual,
			err,
		)
	}
}

func TestAmbiguousAuthorizationCleanupPreservesReplacementAfterRevoke(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement := pending
	replacement.RefreshToken = "replacement-refresh"
	transport := roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		body := ""
		status := http.StatusOK
		switch request.URL.Path {
		case "/auth/revoke":
		case "/auth/token":
			if err := store.write(
				context.Background(),
				oauthSecretPendingService,
				replacement,
				SecretStoreForbidUI,
			); err != nil {
				return nil, err
			}
			status = http.StatusBadRequest
			body = `{"error":"invalid_grant"}`
		default:
			return nil, errors.New(
				"unexpected OAuth endpoint",
			)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body: io.NopCloser(
				strings.NewReader(body),
			),
		}, nil
	})
	oauth, err := NewHAOAuthClient(
		productionCloudTestOrigin,
		&http.Client{Transport: transport},
	)
	if err != nil {
		t.Fatal(err)
	}
	metadataCleared := false
	storeErr := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"store new authorization",
		errors.New("worker outcome unknown"),
	)
	got := cleanupUnstoredCloudAuthorization(
		context.Background(),
		oauth,
		store,
		pending.Generation,
		func(string) error {
			metadataCleared = true
			return nil
		},
		pending.RefreshToken,
		pending.ClientID,
		storeErr,
	)
	var problem *cloudProblem
	if !errors.As(got, &problem) ||
		!strings.Contains(
			problem.Cause.Error(),
			string(CloudErrSecretConflict),
		) {
		t.Fatalf("cleanup error = %v", got)
	}
	if metadataCleared {
		t.Fatal("replacement cleanup cleared metadata")
	}
	assertPendingOAuthReplacement(t, store, replacement)
}

func assertPendingOAuthReplacement(
	t *testing.T,
	store *KeyringOAuthSecretStore,
	replacement OAuthSecretEnvelope,
) {
	t.Helper()
	actual, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists ||
		!sameOAuthSecretEnvelope(actual, replacement) {
		t.Fatalf(
			"replacement exists=%v value=%+v err=%v",
			exists,
			actual,
			err,
		)
	}
}
