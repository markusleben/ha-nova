package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

const marketplaceSayEN = "Not done: HA NOVA can't install Marketplace updates yet, because Home Assistant first asks for the Marketplace warning to be read and accepted. Nothing changed. Update it in Home Assistant: Settings → Marketplace. HA NOVA will support this in a later release."

// marketplaceHA: a home with a Marketplace update and a HACS one; info is
// what marketplace/info answers (nil: the read fails).
func marketplaceHA(info map[string]any) *fakeHA {
	f := newFakeHA()
	f.ent("update.card_mod_update", "marketplace", "on", map[string]any{"friendly_name": "card-mod update"})
	f.ent("update.mushroom_update", "hacs", "on", map[string]any{"friendly_name": "Mushroom update"})
	f.services = append(f.defaultServices(), map[string]any{"domain": "update", "services": map[string]any{
		"install": map[string]any{"target": map[string]any{}}, "skip": map[string]any{"target": map[string]any{}}}})
	if info == nil {
		f.fail["marketplace/info"] = true
	} else {
		f.extra = func(endpoint string, p map[string]any) (any, bool) {
			return info, endpoint == "ws" && p["type"] == "marketplace/info"
		}
	}
	return f
}

func marketplaceReads(h *writeHarness) int {
	n := 0
	for _, c := range h.log.all() {
		if c.payload["type"] == "marketplace/info" {
			n++
		}
	}
	return n
}

func wantMarketplaceRefusal(t *testing.T, h *writeHarness, exit int, env verbTestEnvelope, say string) {
	t.Helper()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, "refused (MARKETPLACE_WARNING)") ||
		env.Error.Next != marketplaceNext || env.Say != say || env.Plan != nil {
		t.Fatalf("exit %d say %q error %+v plan %+v", exit, env.Say, env.Error, env.Plan)
	}
	if !slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == reasonMarketplaceWarning }) {
		t.Fatalf("reasons %+v", env.Reasons)
	}
	if len(h.planFiles()) != 0 {
		t.Fatalf("a refused plan was stored: %v", h.planFiles())
	}
}

// The bug from a real home (HA 2026.11.1): the Relay's user never read the
// Marketplace warning, so Home Assistant answers the install with HTTP 500.
// The plan says so up front and sends nothing.
func TestMarketplaceUpdateRefusedWhenWarningNotAccepted(t *testing.T) {
	h := newWriteHarness(t, marketplaceHA(map[string]any{"warning_accepted": false, "version": "2026.11.1"}))
	exit, env := h.run(runPlanCommand, "call", "update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
	wantMarketplaceRefusal(t, h, exit, env, marketplaceSayEN)
	exit, env = h.run(runCallCommand, "update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
	wantMarketplaceRefusal(t, h, exit, env, marketplaceSayEN)
	if len(h.posts()) != 0 {
		t.Fatalf("posts %v", h.posts())
	}
}

// A read that fails, or an answer without the flag, refuses the same way.
func TestMarketplaceUpdateRefusedWhenInfoUnread(t *testing.T) {
	for name, info := range map[string]map[string]any{"read fails": nil, "no flag": {"version": "2026.11.1"}} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, marketplaceHA(info))
			exit, env := h.run(runPlanCommand, "call", "update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
			wantMarketplaceRefusal(t, h, exit, env, marketplaceSayEN)
			if len(h.posts()) != 0 {
				t.Fatalf("posts %v", h.posts())
			}
		})
	}
}

// The say speaks Home Assistant's language.
func TestMarketplaceUpdateRefusalSpeaksGerman(t *testing.T) {
	f := marketplaceHA(map[string]any{"warning_accepted": false})
	f.config = map[string]any{"language": "de"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "call", "update.install", "update.card_mod_update", "--skill", "ha-nova:updates")
	wantMarketplaceRefusal(t, h, exit, env, confirmT("de", "say.marketplace_update"))
	if !strings.HasPrefix(env.Say, "Nicht erledigt: HA NOVA kann Marketplace-Updates noch nicht installieren") {
		t.Fatalf("say %q", env.Say)
	}
}

// Neither a HACS install nor a Marketplace skip reads the Marketplace:
// Home Assistant checks the warning on a Marketplace install only.
func TestMarketplaceWarningLeavesOtherUpdatesAlone(t *testing.T) {
	h := newWriteHarness(t, marketplaceHA(map[string]any{"warning_accepted": false}))
	hacs := h.plan("update.install", "update.mushroom_update", "--skill", "ha-nova:updates")
	skip := h.plan("update.skip", "update.card_mod_update", "--skill", "ha-nova:updates")
	if hacs.Tier != "typed" || !strings.Contains(fmt.Sprint(hacs.Reasons), reasonHACSInstall) || skip.Tier == "refused" || marketplaceReads(h) != 0 {
		t.Fatalf("hacs %s %v, skip %s %v, marketplace reads %d", hacs.Tier, hacs.Reasons, skip.Tier, skip.Reasons, marketplaceReads(h))
	}
}

// Accepted at plan time, Home Assistant still says no at apply (the
// acceptance was withdrawn): its HTTP 500, as JSON or plain text, becomes
// the same refusal, not a raw HTTP error, and nothing is sent again.
func TestMarketplaceUpdateApplyMapsTheWarningError(t *testing.T) {
	const message = "Open the Marketplace and read the warning first, installing and updating work once it is accepted."
	for name, body := range map[string]any{"json": map[string]any{"message": message}, "text": "500 Internal Server Error\n\n" + message} {
		t.Run(name, func(t *testing.T) {
			f := marketplaceHA(map[string]any{"warning_accepted": true})
			f.service = func(*fakeHA, string, map[string]any) any { return coreReply(500, body) }
			h := newWriteHarness(t, f)
			plan := h.plan("update.install", "update.card_mod_update", "--skill", "ha-nova:updates").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code)
			if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, "refused (MARKETPLACE_WARNING)") ||
				!strings.Contains(env.Error.Message, "HTTP 500") || env.Error.Next != marketplaceNext || env.Say != marketplaceSayEN || len(h.posts()) != 1 {
				t.Fatalf("exit %d say %q error %+v posts %d", exit, env.Say, env.Error, len(h.posts()))
			}
		})
	}
}

// Several targets install at once and Home Assistant reports the first
// failure, so a warning error there proves nothing about the others: it stays
// Home Assistant's unverified error, never "nothing changed".
func TestMarketplaceUpdateApplyKeepsAMultiTargetError(t *testing.T) {
	f := marketplaceHA(map[string]any{"warning_accepted": true})
	f.service = func(*fakeHA, string, map[string]any) any {
		return coreReply(500, map[string]any{"message": "Open the Marketplace and read the warning first, installing and updating work once it is accepted."})
	}
	h := newWriteHarness(t, f)
	plan := h.plan("update.install", "update.card_mod_update", "update.mushroom_update", "--skill", "ha-nova:updates").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); env.Error == nil || env.Error.Code != "SERVICE_ERROR" || exit == verbExitRefused || env.Say == marketplaceSayEN {
		t.Fatalf("exit %d say %q error %+v", exit, env.Say, env.Error)
	}
}

// Any other HTTP 500 on a Marketplace install stays Home Assistant's error.
func TestMarketplaceUpdateApplyKeepsOtherErrors(t *testing.T) {
	f := marketplaceHA(map[string]any{"warning_accepted": true})
	f.service = func(*fakeHA, string, map[string]any) any {
		return coreReply(500, map[string]any{"message": "GitHub's request limit is reached, try again later."})
	}
	h := newWriteHarness(t, f)
	plan := h.plan("update.install", "update.card_mod_update", "--skill", "ha-nova:updates").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); env.Error == nil || env.Error.Code != "SERVICE_ERROR" || exit == verbExitRefused {
		t.Fatalf("exit %d error %+v", exit, env.Error)
	}
}
