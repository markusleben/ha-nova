package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectLocalRelayAuthFailuresGuideSelectedProfileRepair(
	t *testing.T,
) {
	for _, test := range []struct {
		name   string
		status int
		run    func(runtimePaths) int
	}{
		{
			name:   "functional request unauthorized",
			status: http.StatusUnauthorized,
			run: func(paths runtimePaths) int {
				return runRelayProxy(
					paths,
					"ws",
					[]string{
						"--server", "cabin",
						"--via", "local",
						"--data", `{}`,
					},
				)
			},
		},
		{
			name:   "health forbidden",
			status: http.StatusForbidden,
			run: func(paths runtimePaths) int {
				return runHealth(
					paths,
					[]string{
						"--server", "cabin",
						"--via", "local",
					},
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				testV2TwoProfileConfig,
			)
			server := httptest.NewServer(
				http.HandlerFunc(func(
					writer http.ResponseWriter,
					_ *http.Request,
				) {
					writer.WriteHeader(test.status)
					if test.status == http.StatusUnauthorized {
						_, _ = writer.Write([]byte(`not-json`))
					}
				}),
			)
			t.Cleanup(server.Close)
			previous := resolveLocalRelayTransportForCLI
			resolveLocalRelayTransportForCLI = func(
				context.Context,
				runtimeConfig,
			) (relayTransportSelection, error) {
				return relayTransportSelection{
					BaseURL:    server.URL,
					Client:     server.Client(),
					Credential: "device-credential",
					DeviceMode: true,
					Via:        relayViaLocal,
				}, nil
			}
			t.Cleanup(func() {
				resolveLocalRelayTransportForCLI = previous
			})
			exit, output := captureCommandOutput(t, func() int {
				return test.run(paths)
			})
			want := `ha-nova pair --server cabin --relay-url "http://cabin:8791"`
			if exit != 1 || !strings.Contains(output, want) {
				t.Fatalf(
					"exit=%d output=%q",
					exit,
					output,
				)
			}
		})
	}
}
