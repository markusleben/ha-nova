package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func stubRelayTransportResolvers(t *testing.T) (*int, *int, *int) {
	t.Helper()
	localCalls, cloudCalls, automaticCalls := 0, 0, 0
	oldLocal := resolveLocalRelayTransportForCLI
	oldCloud := resolveCloudRelayTransportForCLI
	oldAutomatic := resolveAutomaticRelayTransportForCLI
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		localCalls++
		return relayTransportSelection{BaseURL: "https://local", Client: &http.Client{}, Credential: "local", Via: relayViaLocal}, nil
	}
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		cloudCalls++
		return relayTransportSelection{BaseURL: "https://cloud", Client: &http.Client{}, Credential: "cloud", Via: relayViaCloud}, nil
	}
	resolveAutomaticRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		automaticCalls++
		return relayTransportSelection{BaseURL: "https://auto", Client: &http.Client{}, Credential: "auto", Via: relayViaCloud}, nil
	}
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI = oldLocal
		resolveCloudRelayTransportForCLI = oldCloud
		resolveAutomaticRelayTransportForCLI = oldAutomatic
	})
	return &localCalls, &cloudCalls, &automaticCalls
}

func readyCloudForTransportTest() *cloudLifecycleMetadata {
	metadata := cloudMetadataForTest(strings.Repeat("d", 32))
	return &cloudLifecycleMetadata{State: cloudStateReady, Current: &metadata}
}

func TestRelayViaParsing(t *testing.T) {
	opts, err := parseRelayFlags("ws", []string{"--data", `{}`, "--via", "cloud"})
	if err != nil || !opts.ViaSet || opts.Via != "cloud" {
		t.Fatalf("relay flags = %+v err=%v", opts, err)
	}
	health, err := parseHealthFlags([]string{"--via", "local"})
	if err != nil || !health.ViaSet || health.Via != "local" {
		t.Fatalf("health flags = %+v err=%v", health, err)
	}
	for _, value := range []string{"", "automatic", "remote"} {
		if _, err := parseRelayFlags("ws", []string{"--data", `{}`, "--via", value}); err == nil {
			t.Fatalf("--via %q accepted", value)
		}
	}
}

func TestNamedRelayPairCommandNeverEchoesUnsafeSavedURL(
	t *testing.T,
) {
	const unsafeURL = "http://ha.local;touch-danger"
	command := namedRelayPairCommand("cabin", unsafeURL)
	if strings.Contains(command, unsafeURL) ||
		command != `ha-nova pair --server cabin --relay-url "http://<ha-host>:8791"` {
		t.Fatalf("unsafe pair command = %q", command)
	}
	valid := namedRelayPairCommand(
		"cabin",
		"http://cabin.local:8791",
	)
	if valid !=
		`ha-nova pair --server cabin --relay-url "http://cabin.local:8791"` {
		t.Fatalf("valid pair command = %q", valid)
	}
	ipv6 := namedRelayPairCommand(
		"cabin",
		"http://[fd00::1]:8791",
	)
	if ipv6 !=
		`ha-nova pair --server cabin --relay-url "http://[fd00::1]:8791"` {
		t.Fatalf("IPv6 pair command = %q", ipv6)
	}
}

func TestLocalRelayPreflightWithoutProfileKeepsUsefulRepairCommand(
	t *testing.T,
) {
	err := (&localRelayPreflightError{
		cause: newCloudError(
			CloudErrUnauthorized,
			"test local preflight",
			nil,
		),
	}).Error()
	if !strings.Contains(err, "run: ha-nova setup") {
		t.Fatalf("profile-less preflight error = %q", err)
	}
}

func TestLocalFunctionalAuthFailureUsesSelectedProfileRepair(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")
	cfg := runtimeConfig{
		RelayBaseURL: "http://cabin.local:8791",
	}
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
	} {
		exit, output := captureCommandOutput(t, func() int {
			printLocalRelayAuthRepair(
				status,
				relayTransportSelection{Via: relayViaLocal},
				cfg,
			)
			return 1
		})
		want := `ha-nova pair --server cabin --relay-url "http://cabin.local:8791"`
		if exit != 1 || !strings.Contains(output, want) {
			t.Fatalf(
				"status=%d exit=%d output=%q",
				status,
				exit,
				output,
			)
		}
	}
	_, output := captureCommandOutput(t, func() int {
		printLocalRelayAuthRepair(
			http.StatusUnauthorized,
			relayTransportSelection{Via: relayViaCloud},
			cfg,
		)
		return 1
	})
	if output != "" {
		t.Fatalf("Cloud auth failure printed local repair: %q", output)
	}
}

func TestRelayTransportSelectionHonorsPolicyAndExplicitOverride(t *testing.T) {
	localCalls, cloudCalls, automaticCalls := stubRelayTransportResolvers(t)
	cfg := runtimeConfig{RoutePolicy: routePolicyCloud, Cloud: readyCloudForTransportTest()}

	selected, err := selectRelayTransport(context.Background(), cfg, relayViaLocal, true)
	if err != nil || selected.Via != relayViaLocal {
		t.Fatalf("explicit local = %+v err=%v", selected, err)
	}
	if *localCalls != 1 || *cloudCalls != 0 || *automaticCalls != 0 {
		t.Fatalf("resolver calls local/cloud/automatic = %d/%d/%d", *localCalls, *cloudCalls, *automaticCalls)
	}

	selected, err = selectRelayTransport(context.Background(), cfg, "", false)
	if err != nil || selected.Via != relayViaCloud || *cloudCalls != 1 {
		t.Fatalf("cloud policy = %+v err=%v calls=%d", selected, err, *cloudCalls)
	}

	cfg.RoutePolicy = routePolicyAutomatic
	selected, err = selectRelayTransport(context.Background(), cfg, "", false)
	if err != nil || selected.BaseURL != "https://auto" || *automaticCalls != 1 {
		t.Fatalf("automatic policy = %+v err=%v calls=%d", selected, err, *automaticCalls)
	}
}

func TestRelayCloudSelectionFailsTypedBeforeResolverWhenNotReady(t *testing.T) {
	_, cloudCalls, automaticCalls := stubRelayTransportResolvers(t)
	cfg := runtimeConfig{RoutePolicy: routePolicyCloud}

	_, err := selectRelayTransport(context.Background(), cfg, "", false)
	var problem *cloudProblem
	if !errors.As(err, &problem) || problem.Code != cloudProblemNotConfigured {
		t.Fatalf("error = %T %v", err, err)
	}
	if *cloudCalls != 0 || *automaticCalls != 0 {
		t.Fatalf("Cloud resolver called for unconfigured profile: %d/%d", *cloudCalls, *automaticCalls)
	}
}

func TestRelayCloudSelectionRejectsCompletedRevocationBeforeResolver(
	t *testing.T,
) {
	tests := []struct {
		name        string
		policy      routePolicy
		override    relayVia
		overrideSet bool
	}{
		{name: "route cloud", policy: routePolicyCloud},
		{
			name:        "explicit cloud",
			policy:      routePolicyLocal,
			override:    relayViaCloud,
			overrideSet: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			localCalls, cloudCalls, automaticCalls :=
				stubRelayTransportResolvers(t)
			cloud := readyCloudForTransportTest()
			cloud.DeviceRevocationCompleted =
				&cloudDeviceRevocationCheckpoint{
					CurrentDeviceID: "dev-1234567890abcdef",
				}
			cfg := runtimeConfig{
				RoutePolicy: testCase.policy,
				Cloud:       cloud,
			}
			_, err := selectRelayTransport(
				context.Background(),
				cfg,
				testCase.override,
				testCase.overrideSet,
			)
			var problem *cloudProblem
			if !errors.As(err, &problem) ||
				problem.Remediation != cloudRemediationSecurityStop {
				t.Fatalf("selection error = %T %v", err, err)
			}
			if *localCalls != 0 || *cloudCalls != 0 ||
				*automaticCalls != 0 {
				t.Fatalf(
					"resolver calls local/cloud/automatic = %d/%d/%d",
					*localCalls,
					*cloudCalls,
					*automaticCalls,
				)
			}
		})
	}
}

func TestAutomaticSelectionRejectsCloudResultDuringCleanup(
	t *testing.T,
) {
	localCalls, cloudCalls, automaticCalls :=
		stubRelayTransportResolvers(t)
	cloud := readyCloudForTransportTest()
	cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: "dev-1234567890abcdef",
		}
	_, err := selectRelayTransport(
		context.Background(),
		runtimeConfig{
			RoutePolicy: routePolicyAutomatic,
			Cloud:       cloud,
		},
		"",
		false,
	)
	var problem *cloudProblem
	if !errors.As(err, &problem) ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("selection error = %T %v", err, err)
	}
	if *localCalls != 0 ||
		*cloudCalls != 0 ||
		*automaticCalls != 1 {
		t.Fatalf(
			"resolver calls local/cloud/automatic = %d/%d/%d",
			*localCalls,
			*cloudCalls,
			*automaticCalls,
		)
	}
}

func TestRelayCloudErrorGetsStableRemediationSurface(t *testing.T) {
	message := relayTransportErrorMessage(newCloudError(
		CloudErrSecretStoreLocked,
		"read OAuth secret",
		errors.New("sensitive backend detail"),
	))
	for _, want := range []string{"cloud_secure_storage", "unlock_secure_storage"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q missing %q", message, want)
		}
	}
	if strings.Contains(message, "sensitive backend detail") {
		t.Fatalf("low-level cause leaked into user-facing message: %q", message)
	}
}

func TestRelayCloudIdentityErrorsStopForSecurityReview(t *testing.T) {
	for _, code := range []CloudErrorCode{
		CloudErrIdentityMismatch,
		CloudErrRelayInstance,
		CloudErrDeviceUserConflict,
		CloudErrRedirectRejected,
	} {
		problem := cloudProblemForError(newCloudError(code, "verify Cloud identity", nil))
		if problem.Code != cloudProblemIdentityMismatch ||
			problem.Remediation != cloudRemediationSecurityStop {
			t.Fatalf("%s mapped to code=%s remediation=%s", code, problem.Code, problem.Remediation)
		}
	}
}

func TestRelayTransportRejectsIncompleteResolverResult(t *testing.T) {
	old := resolveCloudRelayTransportForCLI
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{Via: relayViaCloud}, nil
	}
	t.Cleanup(func() { resolveCloudRelayTransportForCLI = old })

	_, err := selectRelayTransport(
		context.Background(),
		runtimeConfig{RoutePolicy: routePolicyCloud, Cloud: readyCloudForTransportTest()},
		"", false,
	)
	if err == nil || !strings.Contains(err.Error(), "incomplete selection") {
		t.Fatalf("incomplete resolver error = %v", err)
	}
}

func TestRelayTransportSelectionPropagatesCallerDeadline(t *testing.T) {
	old := resolveCloudRelayTransportForCLI
	resolveCloudRelayTransportForCLI = func(
		ctx context.Context,
		_ runtimeConfig,
	) (relayTransportSelection, error) {
		<-ctx.Done()
		return relayTransportSelection{}, ctx.Err()
	}
	t.Cleanup(func() { resolveCloudRelayTransportForCLI = old })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := selectRelayTransport(
		ctx,
		runtimeConfig{
			RoutePolicy: routePolicyCloud,
			Cloud:       readyCloudForTransportTest(),
		},
		"",
		false,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selection error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("selection ignored caller deadline: %s", elapsed)
	}
}
