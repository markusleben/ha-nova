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

func TestOAuthWaitReprovesWritableKeyringBeforeCodeExchange(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-oauth-reproof"
	cfg.RelayInstanceID = "relay-oauth-reproof"
	cfg.Cloud = &cloudLifecycleMetadata{
		State: cloudStateAuthorizing,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(
		productionCloudTestOrigin,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend := newMemoryOAuthSecretBackend()
	store, err := NewOAuthSecretStore(
		backend,
		cfg.ProfileID,
	)
	if err != nil {
		t.Fatal(err)
	}

	oldProof := verifyCloudOriginForOAuth
	oldFlow := cloudOAuthFlowForSetup
	oldBrowser := openCloudOAuthBrowserForSetup
	oldExchange := exchangeCloudAuthorizationCodeForSetup
	verifyCloudOriginForOAuth = func(
		context.Context,
		CloudOrigin,
	) error {
		return nil
	}
	cloudOAuthFlowForSetup = func() *OAuthLoopbackFlow {
		flow := NewOAuthLoopbackFlow()
		flow.Timeout = 2 * time.Second
		return flow
	}
	openCloudOAuthBrowserForSetup = func(
		ctx context.Context,
		target string,
	) error {
		return completeOAuthLoopbackForTest(
			ctx,
			target,
			"exchange-after-reproof",
		)
	}
	exchangeCloudAuthorizationCodeForSetup = func(
		context.Context,
		*HAOAuthClient,
		OAuthAuthorizationCode,
	) (HAOAuthToken, error) {
		if release, acquired :=
			acquireAutoRepairLock(paths); acquired {
			release()
			t.Fatal(
				"authorization code exchange ran before mutation lock reacquisition",
			)
		}
		backend.mu.Lock()
		operations := append(
			[]string(nil),
			backend.operations...,
		)
		policies := append(
			[]SecretStoreUIPolicy(nil),
			backend.policies...,
		)
		backend.mu.Unlock()
		wantPolicies := []SecretStoreUIPolicy{
			SecretStoreAllowUI,
			SecretStoreForbidUI,
			SecretStoreForbidUI,
		}
		if strings.Join(operations, ",") !=
			"set,get,delete" ||
			len(policies) != len(wantPolicies) {
			t.Fatalf(
				"pre-exchange keyring operations=%v policies=%v",
				operations,
				policies,
			)
		}
		for index := range wantPolicies {
			if policies[index] != wantPolicies[index] {
				t.Fatalf(
					"pre-exchange policy[%d]=%q want=%q",
					index,
					policies[index],
					wantPolicies[index],
				)
			}
		}
		return HAOAuthToken{
			AccessToken:  "access-after-reproof",
			RefreshToken: "refresh-after-reproof",
			ExpiresAt: time.Now().UTC().Add(
				time.Hour,
			),
		}, nil
	}
	t.Cleanup(func() {
		verifyCloudOriginForOAuth = oldProof
		cloudOAuthFlowForSetup = oldFlow
		openCloudOAuthBrowserForSetup = oldBrowser
		exchangeCloudAuthorizationCodeForSetup = oldExchange
	})

	var accessToken string
	var envelope OAuthSecretEnvelope
	err = withPausableClientMutationLock(
		paths,
		func(mutation *pausableClientMutationLock) error {
			loaded, err :=
				loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				return err
			}
			save := func(value runtimeConfig) error {
				if err := mutation.requireHeld(); err != nil {
					return err
				}
				return saveConfig(paths, value)
			}
			request := newCloudSetupRequest(
				&loaded,
				save,
				mutation,
			)
			accessToken, envelope, err =
				authorizeOrRefreshCloud(
					context.Background(),
					request,
					origin,
					store,
					OAuthSecretEnvelope{},
				)
			return err
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if accessToken != "access-after-reproof" ||
		envelope.RefreshToken != "refresh-after-reproof" {
		t.Fatalf(
			"authorization result access=%q envelope=%+v",
			accessToken,
			envelope,
		)
	}
}

func completeOAuthLoopbackForTest(
	ctx context.Context,
	target string,
	code string,
) error {
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
	query.Set(
		"state",
		authorizationURL.Query().Get("state"),
	)
	query.Set("code", code)
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
	response, err := (&http.Client{
		Timeout: time.Second,
	}).Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
	return err
}
