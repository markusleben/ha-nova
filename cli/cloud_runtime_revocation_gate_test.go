package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutomaticRouteKeepsHealthyLocalDuringCloudCleanup(
	t *testing.T,
) {
	restoreBuild := setCloudFeatureTestBuild(t, true)
	defer restoreBuild()
	configureCloudRemoteFeature(runtimePaths{})

	tests := []struct {
		name       string
		checkpoint func(*cloudLifecycleMetadata)
	}{
		{
			name: "device revocation",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.DeviceRevocationCompleted =
					&cloudDeviceRevocationCheckpoint{
						CurrentDeviceID: "revoked-device",
					}
			},
		},
		{
			name: "authorization revocation",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.AuthorizationRevocationCompleted =
					&cloudAuthorizationRevocationCheckpoint{}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cloud := readyCloudForTransportTest()
			testCase.checkpoint(cloud)
			cfg := runtimeConfig{
				RoutePolicy:     routePolicyAutomatic,
				Cloud:           cloud,
				RelayInstanceID: "relay-local",
			}

			oldLocal := resolveLocalRelayTransportForCLI
			oldCloud := resolveCloudRelayTransportForCLI
			oldStore := newCloudSecretStoreForCLI
			localCalls := 0
			cloudCalls := 0
			server := httptest.NewServer(
				http.HandlerFunc(func(
					writer http.ResponseWriter,
					request *http.Request,
				) {
					if request.URL.Path != "/health" {
						t.Fatalf(
							"local path = %q",
							request.URL.Path,
						)
					}
					_, _ = writer.Write([]byte(
						`{"ok":true,"data":{"relay_instance_id":"relay-local"}}`,
					))
				}),
			)
			defer server.Close()
			resolveLocalRelayTransportForCLI = func(
				context.Context,
				runtimeConfig,
			) (relayTransportSelection, error) {
				localCalls++
				return relayTransportSelection{
					BaseURL:    server.URL,
					Client:     server.Client(),
					Credential: "local-credential",
					Via:        relayViaLocal,
				}, nil
			}
			resolveCloudRelayTransportForCLI = func(
				context.Context,
				runtimeConfig,
			) (relayTransportSelection, error) {
				cloudCalls++
				return relayTransportSelection{},
					errors.New("Cloud resolver must not run")
			}
			newCloudSecretStoreForCLI = func(
				string,
			) (OAuthSecretStore, error) {
				t.Fatal("revoked profile opened OAuth secure storage")
				return nil, nil
			}
			t.Cleanup(func() {
				resolveLocalRelayTransportForCLI = oldLocal
				resolveCloudRelayTransportForCLI = oldCloud
				newCloudSecretStoreForCLI = oldStore
			})

			selection, err := resolveAutomaticRelayTransport(
				context.Background(),
				cfg,
			)
			if err != nil ||
				selection.Via != relayViaLocal {
				t.Fatalf(
					"automatic local selection=%+v err=%v",
					selection,
					err,
				)
			}
			if localCalls != 1 || cloudCalls != 0 {
				t.Fatalf(
					"resolver calls local/cloud = %d/%d",
					localCalls,
					cloudCalls,
				)
			}
			if _, err := resolveCloudRelayTransport(
				context.Background(),
				cfg,
			); err == nil {
				t.Fatal("Cloud resolver accepted revoked profile")
			}
		})
	}
}

func TestAutomaticRouteBlocksCloudFallbackDuringCleanup(
	t *testing.T,
) {
	cloud := readyCloudForTransportTest()
	cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: "revoked-device",
		}
	cfg := runtimeConfig{
		RoutePolicy:     routePolicyAutomatic,
		Cloud:           cloud,
		RelayInstanceID: "relay-local",
	}
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	client := server.Client()
	server.Close()

	oldLocal := resolveLocalRelayTransportForCLI
	oldCloud := resolveCloudRelayTransportForCLI
	resolveLocalRelayTransportForCLI = func(
		context.Context,
		runtimeConfig,
	) (relayTransportSelection, error) {
		return relayTransportSelection{
			BaseURL:    url,
			Client:     client,
			Credential: "local-credential",
			Via:        relayViaLocal,
		}, nil
	}
	cloudCalls := 0
	resolveCloudRelayTransportForCLI = func(
		context.Context,
		runtimeConfig,
	) (relayTransportSelection, error) {
		cloudCalls++
		return relayTransportSelection{}, nil
	}
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI = oldLocal
		resolveCloudRelayTransportForCLI = oldCloud
	})

	_, err := resolveAutomaticRelayTransport(
		context.Background(),
		cfg,
	)
	var problem *cloudProblem
	if !errors.As(err, &problem) ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("automatic fallback error = %T %v", err, err)
	}
	if cloudCalls != 0 {
		t.Fatalf("Cloud resolver called %d times", cloudCalls)
	}
}
