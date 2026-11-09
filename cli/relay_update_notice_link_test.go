package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

// A home reached only over Home Assistant Link: `ha-nova update` must still
// say the NOVA App needs its update (the home connection is unreachable), and
// must not start the guided install, which runs only at home.
func TestUpdateNamesTheAppUpdateOverLinkWithoutTheHomeOnlyOffer(t *testing.T) {
	paths := guidedUpdatePaths(t)
	rememberLinkRouteInTemp(t)
	cfg := automaticLinkTestConfig()
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.RelaySpkiPin = strings.Repeat("p", 43)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	pending := pendingRelayUpdateEntity("update.nova_relay_update", "0.3.0", "0.4.0")
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.3.0"}}`)
		case "/core":
			w.Write(statesEnvelope(t, []map[string]interface{}{pending}))
		case "/ws":
			w.Write(registryEnvelope(t, []map[string]interface{}{novaRegistryEntry("update.nova_relay_update")}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer app.Close()

	oldLocal, oldCloud := resolveLocalRelayTransportForCLI, resolveCloudRelayTransportForCLI
	oldIn, oldOut, oldInput, oldOutput := relayUpdateInputIsTTY, relayUpdateOutputIsTTY, relayUpdateInput, relayUpdateOutput
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI, resolveCloudRelayTransportForCLI = oldLocal, oldCloud
		relayUpdateInputIsTTY, relayUpdateOutputIsTTY, relayUpdateInput, relayUpdateOutput = oldIn, oldOut, oldInput, oldOutput
	})
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		selected := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, syscall.EHOSTUNREACH
		}), relayViaLocal)
		selected.DeviceMode = true
		return selected, nil
	}
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{BaseURL: app.URL, Client: app.Client(), Credential: validCredential(31), DeviceMode: true, Via: relayViaCloud}, nil
	}

	notice := relayUpdateNotice(paths)
	if notice.kind != humanNoticeKindRelayUpdateAvailable || !notice.viaLink || !strings.Contains(notice.message, relayAppUpdatePath) {
		t.Fatalf("notice over Link = %+v", notice)
	}

	relayUpdateInputIsTTY = func() bool { return true }
	relayUpdateOutputIsTTY = func() bool { return true }
	var asked bytes.Buffer
	relayUpdateInput = func() *bufio.Reader { return bufio.NewReader(strings.NewReader("\n")) }
	relayUpdateOutput = func() io.Writer { return &asked }
	offerRelayUpdate(paths, notice)
	if asked.Len() != 0 {
		t.Fatalf("the home-only guided update ran over Link: %q", asked.String())
	}

	_, appNotice := withAppUpdateToo(humanNotice{kind: humanNoticeKindUpdateAvailable, message: "HA NOVA 0.30.0 is out.\nTo update, run: ha-nova update"}, notice)
	if appNotice.empty() {
		t.Fatal("check-update folded the App update into `ha-nova update`, which offers it only at home")
	}
}
