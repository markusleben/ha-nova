package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthWaitReleasesLockAndRejectsCodeAfterConfigChange(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-oauth-lock"
	cfg.RelayInstanceID = "relay-oauth-lock"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())

	oldProof := verifyCloudOriginForOAuth
	oldFlow := cloudOAuthFlowForSetup
	oldBrowser := openCloudOAuthBrowserForSetup
	oldExchange := exchangeCloudAuthorizationCodeForSetup
	verifyCloudOriginForOAuth = func(context.Context, CloudOrigin) error {
		return nil
	}
	cloudOAuthFlowForSetup = func() *OAuthLoopbackFlow {
		flow := NewOAuthLoopbackFlow()
		flow.Timeout = 2 * time.Second
		return flow
	}
	exchangeCalled := false
	exchangeCloudAuthorizationCodeForSetup = func(
		context.Context,
		*HAOAuthClient,
		OAuthAuthorizationCode,
	) (HAOAuthToken, error) {
		exchangeCalled = true
		return HAOAuthToken{}, errors.New("exchange must not run")
	}
	openCloudOAuthBrowserForSetup = func(
		ctx context.Context,
		target string,
	) error {
		release, acquired := acquireAutoRepairLock(paths)
		if !acquired {
			t.Fatal("OAuth browser wait kept the client mutation lock")
		}
		top := readTestConfigTopLevel(t, paths)
		top["concurrent_edit"] = []byte(`true`)
		if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
			t.Fatal(err)
		}
		release()

		authorizationURL, err := url.Parse(target)
		if err != nil {
			return err
		}
		callback, err := url.Parse(
			authorizationURL.Query().Get("redirect_uri"),
		)
		if err != nil {
			return err
		}
		query := callback.Query()
		query.Set("state", authorizationURL.Query().Get("state"))
		query.Set("code", "unexchanged-code")
		callback.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			callback.String(),
			nil,
		)
		if err != nil {
			return err
		}
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err == nil {
			_ = response.Body.Close()
		}
		return err
	}
	t.Cleanup(func() {
		verifyCloudOriginForOAuth = oldProof
		cloudOAuthFlowForSetup = oldFlow
		openCloudOAuthBrowserForSetup = oldBrowser
		exchangeCloudAuthorizationCodeForSetup = oldExchange
	})

	var authorizationErr error
	err = withPausableClientMutationLock(
		paths,
		func(mutation *pausableClientMutationLock) error {
			loaded, err := loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				return err
			}
			save := func(value runtimeConfig) error {
				if err := mutation.requireHeld(); err != nil {
					return err
				}
				return saveConfig(paths, value)
			}
			request := newCloudSetupRequest(&loaded, save, mutation)
			_, _, authorizationErr = authorizeOrRefreshCloud(
				context.Background(),
				request,
				origin,
				store,
				OAuthSecretEnvelope{},
			)
			return mutation.requireHeld()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if authorizationErr == nil ||
		!strings.Contains(
			authorizationErr.Error(),
			"authorization code was not exchanged",
		) {
		t.Fatalf("authorization error = %v", authorizationErr)
	}
	if exchangeCalled {
		t.Fatal("authorization code was exchanged after config drift")
	}
}
