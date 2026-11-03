package main

import (
	"fmt"
	"strings"
	"testing"
)

// Home Assistant 2026.11+ ships HACS built in as the Marketplace: the HACS
// skill's gate and inventory read it raw, and every Marketplace write stays
// refused (installs and removals happen in Home Assistant's own UI).
func TestRawRelayMarketplaceIsReadOnly(t *testing.T) {
	for _, body := range []string{`{"type":"marketplace/info"}`, `{"type":"marketplace/repositories/list","categories":["integration"]}`} {
		rawAllowed(t, "ws", body)
	}
	for _, body := range []string{`{"type":"marketplace/repository/install","repository":"123"}`,
		`{"type":"marketplace/repository/uninstall","repository":"123"}`, `{"type":"marketplace/repository/info","repository_id":"123"}`,
		`{"type":"marketplace/warning/accept"}`, `{"type":"marketplace/repositories/add","repository":"a/b","category":"integration"}`} {
		rawRefused(t, "ws", body)
	}
}

// The Marketplace migrates HACS update entities to platform marketplace:
// installing one still brings third-party code, so it takes the code.
func TestMarketplaceUpdateInstallIsTyped(t *testing.T) {
	f := newFakeHA()
	f.ent("update.card_mod_update", "marketplace", "on", map[string]any{"friendly_name": "card-mod update"})
	f.services = append(f.defaultServices(), map[string]any{"domain": "update", "services": map[string]any{
		"install": map[string]any{"target": map[string]any{}}}})
	h := newWriteHarness(t, f)
	got := h.plan("update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
	if got.Tier != "typed" || !strings.Contains(fmt.Sprint(got.Reasons), reasonHACSInstall) {
		t.Fatalf("Marketplace update: tier %s reasons %v say %q", got.Tier, got.Reasons, got.Say)
	}
}
