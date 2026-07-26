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
