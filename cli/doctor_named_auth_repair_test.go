package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoctorNamedProfileAuthFailureUsesPairCommand(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_SERVER", "cabin")
	relay := httptest.NewServer(
		http.HandlerFunc(func(
			writer http.ResponseWriter,
			_ *http.Request,
		) {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"ok":false}`))
		}),
	)
	t.Cleanup(relay.Close)
	stubDoctorTransport(
		t,
		relay.URL,
		validCredential(225),
	)
	exit, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	want := `ha-nova pair --server cabin --relay-url "http://cabin:8791"`
	if exit != 1 || !strings.Contains(output, want) {
		t.Fatalf("doctor exit=%d output=%q", exit, output)
	}
	if strings.Contains(output, "run 'ha-nova setup'") {
		t.Fatalf(
			"doctor suggested rejected named setup: %q",
			output,
		)
	}
}

func TestDoctorNamedProfileClientRepairStaysOnSelectedProfile(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")
	hint := doctorClientRepairHint(
		clientStatus{
			ID:              "codex",
			Label:           "Codex",
			RuntimeDetected: true,
		},
		installSourceBundle,
	)
	if !strings.Contains(
		hint,
		"ha-nova setup --server cabin codex",
	) {
		t.Fatalf("named client repair hint = %q", hint)
	}
	if !namedSetupRequestAllowed(
		completedLocalCloudTestConfig(),
		false,
		"codex",
		false,
		"",
		"",
		"",
		"",
	) {
		t.Fatal("named client-only setup was rejected")
	}
	halfPaired := completedLocalCloudTestConfig()
	halfPaired.RelaySecureBaseURL = ""
	halfPaired.RelaySpkiPin = ""
	if namedSetupRequestAllowed(
		halfPaired,
		false,
		"codex",
		false,
		"",
		"",
		"",
		"",
	) {
		t.Fatal("half-paired named client repair bypassed profile guard")
	}
}

func TestNamedClientRepairRejectsHalfPairedProfileBeforeTokenRead(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, `{
		"schema_version":2,
		"default_server":"default",
		"client_install_id":"inst-abc",
		"servers":{
			"default":{
				"ha_host":"ha",
				"ha_url":"http://ha:8123",
				"relay_base_url":"http://ha:8791"
			},
			"cabin":{
				"ha_host":"cabin",
				"ha_url":"http://cabin:8123",
				"relay_base_url":"http://cabin:8791"
			}
		}
	}`)
	exit, output := captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{
				"--server",
				"cabin",
				"--non-interactive",
				"codex",
			},
		)
	})
	if exit != 1 ||
		!strings.Contains(
			output,
			`setup can use named profile "cabin" only`,
		) {
		t.Fatalf("setup exit=%d output=%q", exit, output)
	}
}

func TestNamedClientRepairSkipsPendingActivationMutation(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	base := completedLocalCloudTestConfig()
	base.ProfileID = "profile-default"
	if err := saveConfig(paths, base); err != nil {
		t.Fatal(err)
	}
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-cabin"
	cfg.RelayInstanceID = "relay-cabin"
	cfg.PendingSecureBaseURL = "https://pending.invalid:8792"
	cfg.PendingSpkiPin = "pending-pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	activationCalls := 0
	previousResume := resumePendingActivationAfterRetirementCheck
	resumePendingActivationAfterRetirementCheck = func(
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error) {
		activationCalls++
		return true, nil
	}
	previousVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool {
		return true
	}
	t.Cleanup(func() {
		resumePendingActivationAfterRetirementCheck = previousResume
		verifyDeviceHealth = previousVerify
	})

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{
				"--server",
				"cabin",
				"--non-interactive",
				"codex",
			},
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "Repaired client integration") ||
		strings.Contains(output, "Resumed the interrupted pairing") {
		t.Fatalf("named client repair exit=%d output=%q", exit, output)
	}
	if activationCalls != 0 {
		t.Fatalf(
			"named client repair resumed activation %d times",
			activationCalls,
		)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PendingSecureBaseURL != cfg.PendingSecureBaseURL ||
		saved.PendingSpkiPin != cfg.PendingSpkiPin {
		t.Fatalf("named client repair mutated pending pairing: %+v", saved)
	}
}

func TestNamedClientRepairNeverRepairsInvalidInstallIdentity(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	base := completedLocalCloudTestConfig()
	base.ProfileID = "profile-default"
	if err := saveConfig(paths, base); err != nil {
		t.Fatal(err)
	}
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	cabin := completedLocalCloudTestConfig()
	cabin.ProfileID = "profile-cabin"
	cabin.RelayInstanceID = "relay-cabin"
	if err := saveConfig(paths, cabin); err != nil {
		t.Fatal(err)
	}
	corruptClientInstallID(t, paths)

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{
				"--server",
				"cabin",
				"--non-interactive",
				"unsupported-client",
			},
		)
	})
	if exit != 1 || !strings.Contains(output, "unsupported client") {
		t.Fatalf("unsupported exit=%d output=%q", exit, output)
	}
	assertInvalidInstallIdentityUnchanged(t, paths)

	exit, output = captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{
				"--server",
				"cabin",
				"--non-interactive",
				"codex",
			},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "no configuration was changed") ||
		!strings.Contains(
			output,
			"ha-nova setup --server cabin",
		) {
		t.Fatalf("client repair exit=%d output=%q", exit, output)
	}
	assertInvalidInstallIdentityUnchanged(t, paths)
}

func assertInvalidInstallIdentityUnchanged(
	t *testing.T,
	paths runtimePaths,
) {
	t.Helper()
	top := readTestConfigTopLevel(t, paths)
	if string(top["client_install_id"]) != `" invalid install id "` {
		t.Fatalf(
			"client_install_id changed to %s",
			top["client_install_id"],
		)
	}
}
