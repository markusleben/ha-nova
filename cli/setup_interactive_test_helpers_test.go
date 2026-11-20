package main

import (
	"net/http"
	"strings"
)

func joinSetupInputs(parts ...[]string) string {
	lines := []string{}
	for _, part := range parts {
		lines = append(lines, part...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func setupWizardRelayInstallPrompts() []string {
	return []string{
		"",
		"",
	}
}

func setupWizardGenerateRelayTokenPrompts() []string {
	return []string{
		"",
		"",
		"",
		"",
		"manual",
		"",
		"",
		"",
	}
}

func setupWizardPasteRelayTokenPrompts(token string) []string {
	return []string{
		"",
		"",
		"",
		"",
		"manual",
		"1",
		token,
	}
}

func setupWizardPairingPrompts(code string) []string {
	return []string{
		"",
		"",
		"",
		"",
		code,
	}
}

// serveStandaloneTokenGate answers GET /pair/v1/info as a standalone container
// does: its token gate refuses the unauthenticated probe.
func serveStandaloneTokenGate(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/pair/v1/info" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"UNAUTHORIZED","message":"Missing authorization header"}}`))
	return true
}

// serveAppPairInfo answers GET /pair/v1/info as a NOVA App does.
func serveAppPairInfo(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/pair/v1/info" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_version":"0.7.0","protocol_version":"v1","available":true}}`))
	return true
}
