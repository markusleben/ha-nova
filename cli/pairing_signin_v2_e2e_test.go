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
	ingress, bootstrapURL := startSignInPairingHarness(t)
	meta := deviceMetadata{Name: "signin-e2e", Platform: "linux", Client: "cli", ClientInstallID: "install-signin-e2e"}

	// The armed secret is useless on the LAN pairing port.
	armed, err := armSignInPairing(context.Background(), ingress, CloudEndpointPairArm)
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

// Home Assistant Link owner pairing without a code: the REAL CLI client arms
// /pair/v2/arm/link, pairs a user-bound Cloud device, activates it over
// ingress, and calls a functional route through ingress with it.
func TestLinkOwnerPairingEndToEnd(t *testing.T) {
	ingress, bootstrapURL := startSignInPairingHarness(t)
	ctx := context.Background()
	relay, err := ingress.RelayInfo(ctx)
	if err != nil {
		t.Fatalf("relay info: %v", err)
	}
	meta := deviceMetadata{Name: "link-e2e", Platform: "linux", Client: "cli", ClientInstallID: "install-link-e2e"}

	// The Link secret is useless on the LAN pairing port.
	armed, err := armSignInPairing(ctx, ingress, CloudEndpointPairArmLink)
	if err != nil {
		t.Fatalf("arm link: %v", err)
	}
	if _, err := pairDeviceV1(nil, bootstrapURL, armed.PairingSecret, meta); !errors.Is(err, errPairingInactive) {
		t.Fatalf("Link secret on the LAN port: %v", err)
	}

	provisioned, err := pairDeviceV2LinkOwner(ctx, ingress, meta, relay.RelayInstanceID)
	if err != nil {
		t.Fatalf("Link owner pairing: %v", err)
	}
	activated, err := ingress.ActivateDevice(ctx, provisioned.Credential, relay.RelayInstanceID)
	if err != nil || activated.DeviceID != provisioned.DeviceID {
		t.Fatalf("activate = %+v, %v", activated, err)
	}
	response, err := ingress.Do(ctx, CloudEndpointHealth, provisioned.Credential, nil)
	if err != nil {
		t.Fatalf("functional call: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("functional call with the Link credential = %d", response.StatusCode)
	}

	// Exactly once: the consumed secret pairs nothing more.
	if _, err := pairV2Exchange(ctx, ingress, armed.PairingSecret, meta, relay.RelayInstanceID); err == nil {
		t.Fatal("a consumed Link secret paired again")
	}
}

// startSignInPairingHarness returns an ingress client acting as the owner and
// the LAN bootstrap URL of the real relay listeners.
func startSignInPairingHarness(t *testing.T) (*CloudIngressClient, string) {
	t.Helper()
	harness := os.Getenv("HA_NOVA_PAIR_E2E_HARNESS")
	if harness == "" {
		t.Skip("set HA_NOVA_PAIR_E2E_HARNESS to the relay harness path to run the pairing e2e")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	cmd := exec.Command("node", harness, t.TempDir())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) // registered after t.TempDir, so it runs before that cleanup
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
	return ingress, fmt.Sprintf("http://127.0.0.1:%d", info.BootstrapPort)
}
