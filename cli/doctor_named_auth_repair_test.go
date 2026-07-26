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

func TestDoctorNamedProfileClientRepairEscapesProfileGuard(
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
		"ha-nova setup --server default codex",
	) {
		t.Fatalf("named client repair hint = %q", hint)
	}
}
