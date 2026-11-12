package main

import (
	"fmt"
	"strings"
	"testing"
)

// Home Assistant 2026.11+ ships HACS built in as the Marketplace: the HACS
// skill's gate and inventory read it raw; accepting its warning, installing,
// and uninstalling go only through a typed plan (ha-nova relay refuses
// them, naming the plan), and every other Marketplace write stays refused.
func TestRawRelayMarketplaceLifecycleIsPlanOnly(t *testing.T) {
	for _, body := range []string{`{"type":"marketplace/info"}`, `{"type":"marketplace/repositories/list","categories":["integration"]}`} {
		rawAllowed(t, "ws", body)
	}
	for _, body := range []string{`{"type":"marketplace/repository/info","repository_id":"123"}`,
		`{"type":"marketplace/repositories/add","repository":"a/b","category":"integration"}`} {
		rawRefused(t, "ws", body)
	}
	paths, sent := marketplaceRelay(t, true)
	for _, body := range []string{marketplaceAccept, marketplaceInstall, marketplaceUninstall} {
		payload, _ := decodeObject([]byte(body))
		refusal, err := relayTyped(paths, verbOptions{}, "ws", []byte(body), payload)
		if err != nil || refusal == nil || !strings.Contains(refusal.Error(), "plan") {
			t.Errorf("%s: refusal %v err %v", body, refusal, err)
		}
	}
	if len(sent()) != 0 {
		t.Fatalf("sent %v", sent())
	}
}

// The Marketplace migrates HACS update entities to platform marketplace:
// installing one still brings third-party code, so it takes the code.
func TestMarketplaceUpdateInstallIsTyped(t *testing.T) {
	h := newWriteHarness(t, marketplaceHA(map[string]any{"warning_accepted": true}))
	got := h.plan("update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
	if got.Tier != "typed" || !strings.Contains(fmt.Sprint(got.Reasons), reasonHACSInstall) ||
		!strings.Contains(got.Say, "This installs code from the Marketplace that runs in Home Assistant") || strings.Contains(got.Say, "HACS") {
		t.Fatalf("Marketplace update: tier %s reasons %v say %q", got.Tier, got.Reasons, got.Say)
	}
}

// A Marketplace install says the Marketplace in both languages; a HACS
// install keeps its own clause.
func TestMarketplaceInstallSaysTheMarketplace(t *testing.T) {
	e := newTierEngine(nil)
	e.reg["update.card_mod_update"] = registryEntry{pl: "marketplace"}
	e.reg["update.mushroom_update"] = registryEntry{pl: "hacs"}
	for _, c := range []struct{ lang, id, want string }{
		{"en", "update.card_mod_update", "This installs code from the Marketplace that runs in Home Assistant"},
		{"de", "update.card_mod_update", "Das installiert Code aus dem Marketplace, der in Home Assistant läuft"},
		{"en", "update.mushroom_update", "This installs code from HACS that runs in Home Assistant"},
		{"de", "update.mushroom_update", "Das installiert Code aus HACS, der in Home Assistant läuft"},
	} {
		if got, typed := sayReason(c.lang, e, &verbEnvelope{}, storedPlan{}, tierReason{Code: reasonHACSInstall, EntityID: c.id}); got != c.want || !typed {
			t.Errorf("%s %s: say %q typed %v, want %q", c.lang, c.id, got, typed, c.want)
		}
	}
}
