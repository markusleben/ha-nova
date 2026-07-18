package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in end-to-end test of the REAL cli pairing client against the REAL relay
// handlers. It runs only when HA_NOVA_PAIR_E2E_HARNESS points at the relay
// harness (a node script that stands up the real handlers on two listeners) and
// node is available; otherwise it skips, so `go test ./...` stays hermetic in CI.
// The full OPAQUE/AEAD/TLS interop is also proven byte-for-byte by the spikes;
// this asserts the shipped cli functions drive the whole lifecycle.
func TestPairingClientV1EndToEnd(t *testing.T) {
	harness := os.Getenv("HA_NOVA_PAIR_E2E_HARNESS")
	if harness == "" {
		t.Skip("set HA_NOVA_PAIR_E2E_HARNESS to the relay harness path to run the pairing e2e")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	cmd := exec.Command("node", harness)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	var info struct {
		BootstrapPort int    `json:"bootstrapPort"`
		SecurePort    int    `json:"securePort"`
		Code          string `json:"code"`
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("harness did not print its info line: %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &info); err != nil {
		t.Fatalf("bad harness info %q: %v", line, err)
	}
	bootstrapURL := fmt.Sprintf("http://127.0.0.1:%d", info.BootstrapPort)

	// 1) pair with the real cli client.
	prov, err := pairDeviceV1(nil, bootstrapURL, info.Code, deviceMetadata{Name: "cli-e2e", Platform: "darwin", Client: "claude", ClientInstallID: "install-cli-e2e"})
	if err != nil {
		t.Fatalf("pairDeviceV1: %v", err)
	}
	secureBase := fmt.Sprintf("https://127.0.0.1:%d", prov.SecurePort)

	// 2) device credential must be refused over plain HTTP.
	if code := plainFunctional(t, bootstrapURL, prov.Credential); code != http.StatusForbidden {
		t.Fatalf("plain-http functional expected 403, got %d", code)
	}

	// 3) activate + functional over pinned TLS.
	if err := activateDeviceV1(secureBase, prov.SpkiPin, prov.Credential); err != nil {
		t.Fatalf("activateDeviceV1: %v", err)
	}
	if code := securedFunctional(t, secureBase, prov.SpkiPin, prov.Credential); code != http.StatusOK {
		t.Fatalf("secured functional expected 200, got %d", code)
	}

	// 4) revoke-self, then functional is unauthorized.
	if err := revokeSelfDeviceV1(secureBase, prov.SpkiPin, prov.Credential); err != nil {
		t.Fatalf("revokeSelfDeviceV1: %v", err)
	}
	if code := securedFunctional(t, secureBase, prov.SpkiPin, prov.Credential); code != http.StatusUnauthorized {
		t.Fatalf("post-revoke functional expected 401, got %d", code)
	}
}

// Proves the full orchestration (pending -> activate -> promote + config
// persistence) end-to-end with the real cli functions and the real handlers.
func TestRunSecurePairingEndToEnd(t *testing.T) {
	harness := os.Getenv("HA_NOVA_PAIR_E2E_HARNESS")
	if harness == "" {
		t.Skip("set HA_NOVA_PAIR_E2E_HARNESS to run the pairing e2e")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	cmd := exec.Command("node", harness)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	var info struct {
		BootstrapPort int    `json:"bootstrapPort"`
		Code          string `json:"code"`
	}
	line, _ := bufio.NewReader(stdout).ReadString('\n')
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &info); err != nil {
		t.Fatalf("bad harness info %q: %v", line, err)
	}
	bootstrapURL := fmt.Sprintf("http://127.0.0.1:%d", info.BootstrapPort)

	cfg := &runtimeConfig{}
	saves := 0
	save := func(*runtimeConfig) error { saves++; return nil }
	deviceID, err := runSecurePairing(bootstrapURL, info.Code, cfg, save, defaultPairingClientInfo())
	if err != nil {
		t.Fatalf("runSecurePairing: %v", err)
	}
	if deviceID == "" || cfg.RelaySecureBaseURL == "" || cfg.RelaySpkiPin == "" || cfg.ClientInstallID == "" {
		t.Fatalf("config not populated: %+v", cfg)
	}
	// Current slot holds the credential; pending is cleared.
	cur, ok, _ := readDeviceCredential()
	if !ok || parseDeviceCredential(cur) == nil {
		t.Fatalf("current credential not stored")
	}
	if _, pendingOK, _ := readPendingDeviceCredential(); pendingOK {
		t.Fatalf("pending slot not cleared after promotion")
	}
	// The stored credential authorizes a functional call over the stored pinned endpoint.
	if code := securedFunctional(t, cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cur); code != http.StatusOK {
		t.Fatalf("stored credential functional expected 200, got %d", code)
	}
}

func plainFunctional(t *testing.T, base, cred string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/ws", nil)
	req.Header.Set("authorization", "Bearer "+cred)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func securedFunctional(t *testing.T, secureBase, pin, cred string) int {
	t.Helper()
	code, err := pairAuthedPost(spkiPinnedClient(pin), secureBase+"/ws", cred)
	if err != nil {
		t.Fatal(err)
	}
	return code
}
