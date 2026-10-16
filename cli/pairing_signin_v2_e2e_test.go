package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Opt-in end-to-end test of Home Assistant sign-in pairing with the REAL CLI
// client against the REAL relay ingress, pairing, and device listeners (the
// harness stands in only for Home Assistant + Supervisor ingress forwarding).
// Proves Go<->TS interop of /pair/v2/arm + armed pair-v2 and the shared local
// tail: pending -> pinned-TLS activation -> promotion -> functional 200.
func TestSignInArmedPairingEndToEnd(t *testing.T) {
	harness := os.Getenv("HA_NOVA_PAIR_E2E_HARNESS")
	if harness == "" {
		t.Skip("set HA_NOVA_PAIR_E2E_HARNESS to the relay harness path to run the pairing e2e")
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
		BootstrapPort  int    `json:"bootstrapPort"`
		IngressPort    int    `json:"ingressPort"`
		IngressRoot    string `json:"ingressRoot"`
		IngressSession string `json:"ingressSession"`
	}
	line, _ := bufio.NewReader(stdout).ReadString('\n')
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &info); err != nil {
		t.Fatalf("bad harness info %q: %v", line, err)
	}
	ingress, err := newCloudIngressClientForOrigin(
		&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", info.IngressPort)},
		info.IngressRoot,
		info.IngressSession,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapURL := fmt.Sprintf("http://127.0.0.1:%d", info.BootstrapPort)
	meta := deviceMetadata{Name: "signin-e2e", Platform: "linux", Client: "cli", ClientInstallID: "install-signin-e2e"}

	// The armed secret is useless on the LAN pairing port.
	armed, err := armSignInPairing(context.Background(), ingress)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if _, err := pairDeviceV1(nil, bootstrapURL, armed.PairingSecret, meta); !errors.Is(err, errPairingInactive) {
		t.Fatalf("armed secret on the LAN port: %v", err)
	}

	// Full sign-in pairing through ingress plus the shared local tail.
	cfg := &runtimeConfig{}
	provision := func(m deviceMetadata) (*provisionedCredential, error) {
		return pairDeviceV2Armed(context.Background(), ingress, m)
	}
	deviceID, err := runSecurePairingWithProvisioner(
		bootstrapURL,
		provision,
		cfg,
		func(*runtimeConfig) error { return nil },
		defaultPairingClientInfo(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatalf("sign-in pairing: %v", err)
	}
	current, ok, _ := readDeviceCredential()
	if !ok || deviceIDOf(current) != deviceID || cfg.RelaySpkiPin == "" ||
		!strings.HasPrefix(cfg.RelaySecureBaseURL, "https://127.0.0.1:") {
		t.Fatalf("pairing not persisted: device=%q cfg=%+v", deviceID, cfg)
	}
	if code := securedFunctional(t, cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, current); code != http.StatusOK {
		t.Fatalf("functional call with the sign-in credential = %d", code)
	}

	// Single use: the consumed slot arms again only through a new request.
	if _, err := pairV2Exchange(context.Background(), ingress, armed.PairingSecret, meta, armed.RelayInstanceID); err == nil {
		t.Fatal("a consumed armed secret paired again")
	}
}
