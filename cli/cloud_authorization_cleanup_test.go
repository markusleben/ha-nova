package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCleanupUnstoredCloudAuthorizationRequiresVerifiedRevocation(
	t *testing.T,
) {
	storeErr := newCloudError(
		CloudErrSecretStore,
		"store new authorization",
		errors.New("locked"),
	)
	for _, testCase := range []struct {
		name       string
		transport  http.RoundTripper
		wantManual bool
	}{
		{
			name: "invalid grant proves cleanup",
			transport: oauthRevocationTransport(
				http.StatusBadRequest,
				`{"error":"invalid_grant"}`,
			),
		},
		{
			name: "successful refresh leaves live grant",
			transport: oauthRevocationTransport(
				http.StatusOK,
				`{"access_token":"still-live","token_type":"Bearer","expires_in":1800}`,
			),
			wantManual: true,
		},
		{
			name: "network failure cannot prove cleanup",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("offline")
			}),
			wantManual: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			oauth, err := NewHAOAuthClient(
				productionCloudTestOrigin,
				&http.Client{Transport: testCase.transport},
			)
			if err != nil {
				t.Fatal(err)
			}
			got := cleanupUnstoredCloudAuthorization(
				context.Background(),
				oauth,
				nil,
				"",
				nil,
				"unstored-refresh-secret",
				productionCloudTestClientID,
				storeErr,
			)
			if !testCase.wantManual {
				if got != storeErr {
					t.Fatalf("cleanup result = %v, want original store error", got)
				}
				return
			}
			var problem *cloudProblem
			if !errors.As(got, &problem) ||
				problem.Remediation != cloudRemediationSecurityStop ||
				!strings.Contains(problem.Detail, "revoke its HA NOVA session") ||
				!errors.Is(got, storeErr) {
				t.Fatalf("manual cleanup result = %#v", got)
			}
			if strings.Contains(got.Error(), "unstored-refresh-secret") {
				t.Fatal("cleanup error leaked the refresh token")
			}
		})
	}
}

func TestCleanupAmbiguousCloudAuthorizationClearsSecretAndCheckpoint(
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
	oauth, err := NewHAOAuthClient(
		productionCloudTestOrigin,
		&http.Client{Transport: oauthRevocationTransport(
			http.StatusBadRequest,
			`{"error":"invalid_grant"}`,
		)},
	)
	if err != nil {
		t.Fatal(err)
	}
	storeErr := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"store new authorization",
		errors.New("worker outcome unknown"),
	)
	clearedGeneration := ""
	got := cleanupUnstoredCloudAuthorization(
		context.Background(),
		oauth,
		store,
		pending.Generation,
		func(generation string) error {
			clearedGeneration = generation
			return nil
		},
		pending.RefreshToken,
		pending.ClientID,
		storeErr,
	)
	if got != storeErr {
		t.Fatalf("cleanup result = %v, want original store error", got)
	}
	if clearedGeneration != pending.Generation {
		t.Fatalf(
			"cleared generation = %q, want %q",
			clearedGeneration,
			pending.Generation,
		)
	}
	if _, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("pending after cleanup: exists=%v err=%v", exists, err)
	}
}

func TestCleanupAmbiguousCloudAuthorizationRetainsCheckpointOnDeleteFailure(
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
	deleteErr := errors.New("simulated delete failure")
	backend.fail = func(operation, service string) error {
		if operation == "delete" && service == oauthSecretPendingService {
			return deleteErr
		}
		return nil
	}
	oauth, err := NewHAOAuthClient(
		productionCloudTestOrigin,
		&http.Client{Transport: oauthRevocationTransport(
			http.StatusBadRequest,
			`{"error":"invalid_grant"}`,
		)},
	)
	if err != nil {
		t.Fatal(err)
	}
	storeErr := newCloudError(
		CloudErrSecretOutcomeUnknown,
		"store new authorization",
		errors.New("worker outcome unknown"),
	)
	metadataCleared := false
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
		problem.Remediation != cloudRemediationSecurityStop ||
		!errors.Is(got, deleteErr) {
		t.Fatalf("cleanup result = %#v", got)
	}
	if metadataCleared {
		t.Fatal("metadata cleared before pending secret deletion was confirmed")
	}
	backend.fail = nil
	if _, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || !exists {
		t.Fatalf("pending checkpoint lost: exists=%v err=%v", exists, err)
	}
}

func TestRevokedAuthorizingResumeClearsPendingSecretAndCheckpoint(
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
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	cleared := false
	request := cloudSetupRequest{
		Config: runtimeConfig{
			ProfileID: "profile-1",
			Cloud: &cloudLifecycleMetadata{
				State:   cloudStateAuthorizing,
				Pending: &metadata,
			},
		},
		ClearPendingAuthorization: func(generation string) error {
			if generation != pending.Generation {
				t.Fatalf("cleared generation = %q", generation)
			}
			cleared = true
			return nil
		},
	}
	originalClient := cloudHTTPClientForCLI
	cloudHTTPClientForCLI = &http.Client{Transport: oauthRevocationTransport(
		http.StatusBadRequest,
		`{"error":"invalid_grant"}`,
	)}
	t.Cleanup(func() {
		cloudHTTPClientForCLI = originalClient
	})

	_, _, err = authorizeOrRefreshCloud(
		context.Background(),
		request,
		origin,
		store,
		pending,
	)
	if !IsCloudErrorCode(err, CloudErrOAuthInvalidGrant) {
		t.Fatalf("resume error = %v", err)
	}
	if !cleared {
		t.Fatal("revoked pending metadata was not cleared")
	}
	if _, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("revoked pending secret remains: exists=%v err=%v", exists, err)
	}
}

func oauthRevocationTransport(
	refreshStatus int,
	refreshBody string,
) http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := ""
		switch request.URL.Path {
		case "/auth/revoke":
		case "/auth/token":
			status = refreshStatus
			body = refreshBody
		default:
			return nil, errors.New("unexpected OAuth endpoint")
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
}
