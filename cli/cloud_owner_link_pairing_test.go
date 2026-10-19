package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The Link owner pairs without a code (docs/work/2026-10-18-onboarding-0.27-spec.md
// §7.2); everyone else, and every definitive refusal, keeps the owner code.

func installLinkOwnerPairingHook(
	t *testing.T,
	result func(deviceMetadata) (*cloudProvisionedCredential, error),
) *int {
	t.Helper()
	previous := pairDeviceV2LinkOwnerForCloudSetup
	t.Cleanup(func() { pairDeviceV2LinkOwnerForCloudSetup = previous })
	calls := 0
	pairDeviceV2LinkOwnerForCloudSetup = func(
		_ context.Context,
		_ *CloudIngressClient,
		metadata deviceMetadata,
		expected string,
	) (*cloudProvisionedCredential, error) {
		calls++
		if expected != "relay-recovery" {
			t.Fatalf("expected relay = %q", expected)
		}
		return result(metadata)
	}
	return &calls
}

func linkOwnerTestSession() cloudVerifiedSession {
	session := cloudSetupRemoteTestSession(nil)
	session.User = HACurrentUser{ID: "owner-1", Name: "Owner", IsOwner: true}
	return session
}

func provisionLinkOwnerForTest(
	t *testing.T,
	session cloudVerifiedSession,
	prompts *int,
) (*cloudProvisionedCredential, error) {
	t.Helper()
	request := cloudSetupRemoteTestRequest(t, func(cloudRemotePairingPrompt) (string, error) {
		*prompts++
		return "123456", nil
	})
	return provisionRemoteCloudDeviceWithOwnerCode(
		context.Background(),
		session,
		request,
		"relay-recovery",
		"https://unit.ui.nabu.casa/app",
	)
}

func TestLinkOwnerPairsWithoutCodeAndProfileScopedInstallID(t *testing.T) {
	installOwnerPairingStorageHooks(t)
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")
	codePairs := 0
	installOwnerPairingRelayHook(t, func(string) (*cloudProvisionedCredential, error) {
		codePairs++
		return nil, errors.New("code path must not run")
	})
	sent := ""
	linkCalls := installLinkOwnerPairingHook(t, func(metadata deviceMetadata) (*cloudProvisionedCredential, error) {
		sent = metadata.ClientInstallID
		return ownerPairingTestCredential(), nil
	})
	prompts := 0

	request := cloudSetupRemoteTestRequest(t, func(cloudRemotePairingPrompt) (string, error) {
		prompts++
		return "123456", nil
	})
	request.Config.ClientInstallID = "inst-abc"
	got, err := provisionRemoteCloudDeviceWithOwnerCode(
		context.Background(), linkOwnerTestSession(), request,
		"relay-recovery", "https://unit.ui.nabu.casa/app",
	)
	if err != nil || got.DeviceID != ownerPairingTestCredential().DeviceID {
		t.Fatalf("provisioned=%+v err=%v", got, err)
	}
	if *linkCalls != 1 || prompts != 0 || codePairs != 0 {
		t.Fatalf("link=%d prompts=%d code pairs=%d", *linkCalls, prompts, codePairs)
	}
	if sent != "inst-abc-cabin" {
		t.Fatalf("Link owner pairing sent install id %q, want inst-abc-cabin", sent)
	}
}

func TestLinkNonOwnerKeepsTheOwnerCode(t *testing.T) {
	installOwnerPairingStorageHooks(t)
	installOwnerPairingRelayHook(t, func(string) (*cloudProvisionedCredential, error) {
		return ownerPairingTestCredential(), nil
	})
	linkCalls := installLinkOwnerPairingHook(t, func(deviceMetadata) (*cloudProvisionedCredential, error) {
		t.Fatal("a non-owner must not try the no-code path")
		return nil, nil
	})
	session := linkOwnerTestSession()
	session.User.IsOwner = false
	prompts := 0
	if _, err := provisionLinkOwnerForTest(t, session, &prompts); err != nil {
		t.Fatal(err)
	}
	if *linkCalls != 0 || prompts != 1 {
		t.Fatalf("link=%d prompts=%d", *linkCalls, prompts)
	}
}

func TestLinkOwnerFallsBackToTheCodeOnDefinitiveRefusals(t *testing.T) {
	for name, refusal := range map[string]error{
		"older App without the route": errSignInRelayUnsupported,
		"Relay says not the owner":    errSignInNotOwner,
		"pairing unavailable":         errSignInRelayNotReady,
		"slot replaced by a code":     newCloudError(CloudErrPairingInactive, "perform Cloud pairing", nil),
		"secret rejected":             newCloudError(CloudErrPairingRejected, "perform Cloud pairing", nil),
	} {
		t.Run(name, func(t *testing.T) {
			installOwnerPairingStorageHooks(t)
			codePairs := 0
			installOwnerPairingRelayHook(t, func(string) (*cloudProvisionedCredential, error) {
				codePairs++
				return ownerPairingTestCredential(), nil
			})
			linkCalls := installLinkOwnerPairingHook(t, func(deviceMetadata) (*cloudProvisionedCredential, error) {
				return nil, refusal
			})
			prompts := 0
			if _, err := provisionLinkOwnerForTest(t, linkOwnerTestSession(), &prompts); err != nil {
				t.Fatal(err)
			}
			if *linkCalls != 1 || prompts != 1 || codePairs != 1 {
				t.Fatalf("link=%d prompts=%d code pairs=%d", *linkCalls, prompts, codePairs)
			}
		})
	}
}

func TestLinkOwnerStopsOnAmbiguousOrIdentityFailures(t *testing.T) {
	for name, failure := range map[string]error{
		"outcome unknown": newCloudError(CloudErrOutcomeUnknown, "finish Cloud pairing", nil),
		"network":         newCloudError(CloudErrNetwork, "perform Cloud pairing", nil),
		"other Relay":     newCloudError(CloudErrRelayInstance, "verify Link pairing instance", nil),
	} {
		t.Run(name, func(t *testing.T) {
			installOwnerPairingStorageHooks(t)
			installOwnerPairingRelayHook(t, func(string) (*cloudProvisionedCredential, error) {
				t.Fatal("the code path must not run after an ambiguous failure")
				return nil, nil
			})
			installLinkOwnerPairingHook(t, func(deviceMetadata) (*cloudProvisionedCredential, error) {
				return nil, failure
			})
			prompts := 0
			if _, err := provisionLinkOwnerForTest(t, linkOwnerTestSession(), &prompts); !errors.Is(err, failure) {
				t.Fatalf("err = %v", err)
			}
			if prompts != 0 {
				t.Fatalf("prompts = %d", prompts)
			}
		})
	}
}

// The real client: an older App answers 404 from the Relay itself, which reads
// as "unsupported"; a Relay naming another instance is refused before pairing.
func TestPairDeviceV2LinkOwnerDetectsOlderAppAndOtherRelay(t *testing.T) {
	linkClient := func(status int, body string) *CloudIngressClient {
		return newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, CloudPathPairArmLink) {
				t.Fatalf("request = %s %s", request.Method, request.URL.Path)
			}
			header := http.Header{}
			header.Set(relayVersionHeader, "0.26.0")
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
		}))
	}
	meta := deviceMetadata{Name: "laptop", Platform: "darwin", Client: "cli", ClientInstallID: "inst-abc"}

	_, err := pairDeviceV2LinkOwner(context.Background(),
		linkClient(http.StatusNotFound, `{"ok":false,"error":{"code":"NOT_FOUND"}}`), meta, "relay-1")
	if !errors.Is(err, errSignInRelayUnsupported) || !linkOwnerPairingNeedsCode(err) {
		t.Fatalf("older App: %v", err)
	}

	secret := strings.Repeat("s", 43)
	_, err = pairDeviceV2LinkOwner(context.Background(), linkClient(http.StatusOK,
		`{"ok":true,"data":{"pairing_secret":"`+secret+`","expires_in_s":120,"relay_instance_id":"relay-2"}}`),
		meta, "relay-1")
	if !IsCloudErrorCode(err, CloudErrRelayInstance) || linkOwnerPairingNeedsCode(err) {
		t.Fatalf("other Relay: %v", err)
	}
}
