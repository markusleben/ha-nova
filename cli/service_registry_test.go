package main

import (
	"strings"
	"testing"
)

// plugHA: the local-model S3 case — a smart plug whose factory reset is a
// button on the same device; tplink itself registers no services.
func plugHA() *fakeHA {
	f := newFakeHA()
	f.ent("switch.smart_plug", "mqtt", "on", map[string]any{"_di": "dev_plug", "friendly_name": "Smart Plug"})
	f.ent("button.plug_factory_reset", "mqtt", "unknown", map[string]any{"_di": "dev_plug", "friendly_name": "Plug Factory reset"})
	f.ent("button.plug_identify", "mqtt", "unknown", map[string]any{"_di": "dev_plug"})
	f.ent("button.plug_restart", "mqtt", "unknown", map[string]any{"_di": "dev_plug"})
	return f
}

func wantUnlisted(t *testing.T, label string, h *writeHarness, exit int, env verbTestEnvelope, code string) {
	t.Helper()
	if exit != 4 || env.Tier != "refused" || len(env.Reasons) != 1 || env.Reasons[0].Code != code || env.Plan != nil || len(h.planFiles()) != 0 || len(h.posts()) != 0 {
		t.Fatalf("%s: exit %d env %+v plans %v posts %d", label, exit, env, h.planFiles(), len(h.posts()))
	}
}

// Local-model S3: an invented service never reaches a confirmation; the
// refusal names the device's real factory-reset button instead.
func TestInventedServiceIsRefusedWithRealCandidates(t *testing.T) {
	h := newWriteHarness(t, plugHA())
	for label, argv := range map[string][]string{
		"plan call":          {"call", "tplink.smartplug_factory_reset", "switch.smart_plug"},
		"plan call --match":  {"call", "tplink.smartplug_factory_reset", "--match", "smart plug"},
		"plan call --device": {"call", "tplink.smartplug_factory_reset", "--device", "dev_plug"},
	} {
		exit, env := h.run(runPlanCommand, argv...)
		wantUnlisted(t, label, h, exit, env, reasonServiceNotFound)
		if next := env.Error.Next; !strings.Contains(next, "doesn't list the service tplink.smartplug_factory_reset") || !strings.Contains(next, "button.press on button.plug_factory_reset") || strings.Contains(next, "plug_identify") || strings.Contains(next, "plug_restart") {
			t.Fatalf("%s next: %q", label, next)
		}
	}
	// The suggestion matches the operation: a restart (or a bare reset)
	// never offers the factory reset.
	for service, want := range map[string]string{"tplink.restart": "Real candidates: button.press on button.plug_restart.", "tplink.reset": "Read the real services from /api/services first."} {
		_, env := h.run(runPlanCommand, "call", service, "switch.smart_plug")
		if !strings.HasSuffix(env.Error.Next, want) {
			t.Fatalf("%s next: %q", service, env.Error.Next)
		}
	}
	exit, env := h.run(runCallCommand, "tplink.smartplug_factory_reset", "--match", "smart plug")
	wantUnlisted(t, "call --match", h, exit, env, reasonServiceNotFound)

	// No reset wording: the domain's own real services, at most 3.
	exit, env = h.run(runCallCommand, "light.turn_onn", "light.kitchen")
	wantUnlisted(t, "same-domain typo", h, exit, env, reasonServiceNotFound)
	if env.Error.Next != "Home Assistant doesn't list the service light.turn_onn. Real candidates: light.toggle; light.turn_off; light.turn_on." {
		t.Fatalf("same-domain next: %q", env.Error.Next)
	}
}

func TestRealIntegrationServiceStillPlans(t *testing.T) {
	h := newWriteHarness(t, plugHA())
	exit, env := h.run(runPlanCommand, "call", "shelly.blink", "switch.smart_plug")
	if exit != 3 || env.Plan == nil || env.Tier == "refused" || !strings.Contains(reasonCodes(env), reasonIntegrationSvc) {
		t.Fatalf("integration service: exit %d env %+v", exit, env)
	}
}

// Fail closed: an unreadable list proves no service exists.
func TestUnreadableServiceListRefusesEveryCall(t *testing.T) {
	f := plugHA()
	f.fail["/api/services"] = true
	h := newWriteHarness(t, f)
	for _, args := range [][]string{{"light.turn_off", "light.kitchen"}, {"tplink.smartplug_factory_reset", "switch.smart_plug"}, {"homeassistant.check_config"}} {
		exit, env := h.run(runCallCommand, args...)
		wantUnlisted(t, strings.Join(args, " "), h, exit, env, reasonServiceListUnread)
	}
	// On apply the claimed plan is gone: the next step is a fresh plan, never "apply again".
	f.fail["/api/services"] = false
	planned := h.plan("lock.unlock", "lock.front")
	f.fail["/api/services"] = true
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 4 || env.Reasons[0].Code != reasonServiceListUnread || !strings.Contains(env.Error.Next, "Plan again") || len(h.posts()) != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, env)
	}
}

// A service that vanished between plan and apply is refused on recompute.
func TestApplyRefusesAServiceThatVanished(t *testing.T) {
	f := plugHA()
	h := newWriteHarness(t, f)
	planned := h.plan("lock.unlock", "lock.front")
	f.services = []map[string]any{{"domain": "light", "services": map[string]any{"turn_on": map[string]any{}}}}
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 4 || len(env.Reasons) != 1 || env.Reasons[0].Code != reasonServiceNotFound || len(h.posts()) != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, env)
	}
}

// HA saves a config whose step calls a missing service, so plan write
// reports it and still plans the save.
func TestDraftStepWithMissingServiceIsReported(t *testing.T) {
	h := newWriteHarness(t, plugHA())
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("tplink.smartplug_factory_reset")))
	if exit != 3 || env.Plan == nil || !strings.Contains(reasonCodes(env), reasonServiceNotFound) || !strings.Contains(env.Say, "A step calls “tplink.smartplug_factory_reset”, which Home Assistant doesn't have") {
		t.Fatalf("draft: exit %d env %+v", exit, env)
	}
	exit, env = h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	if exit != 3 || strings.Contains(reasonCodes(env), reasonServiceNotFound) {
		t.Fatalf("real draft: exit %d env %+v", exit, env)
	}
}

func TestMCPRefusesInventedServices(t *testing.T) {
	h := newWriteHarness(t, plugHA())
	c := startMCP(t, h.paths)
	mcpInit(c)
	for name, args := range map[string]map[string]any{
		"ha_plan": {"kind": "call", "service": "tplink.smartplug_factory_reset", "entity_ids": []string{"switch.smart_plug"}},
		"ha_call": {"service": "tplink.smartplug_factory_reset", "match": "smart plug"},
	} {
		result := c.tool(name, args)
		env := envelopeOf(result)
		errInfo, _ := env["error"].(map[string]any)
		if result["isError"] != true || env["tier"] != "refused" || env["plan"] != nil || !strings.Contains(errInfo["next"].(string), "button.press on button.plug_factory_reset") || len(h.posts()) != 0 {
			t.Fatalf("%s: %v", name, result)
		}
	}
}
