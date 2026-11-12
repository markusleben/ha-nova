package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// marketplaceRelay: a Relay whose Home Assistant user is the Supervisor
// (supervisor) or a person's account, with one Marketplace repository.
func marketplaceRelay(t *testing.T, supervisor bool) (runtimePaths, func() []string) {
	t.Helper()
	name, system := "Supervisor", true
	if !supervisor {
		name, system = "Nova", false
	}
	return rawRelay(t, map[string]func(map[string]any) any{
		"auth/current_user": func(map[string]any) any { return map[string]any{"id": "u-nova", "name": name} },
		"config/auth/list": func(map[string]any) any {
			return []any{map[string]any{"id": "u-owner", "name": "Owner", "is_owner": true}, map[string]any{"id": "u-nova", "name": name, "system_generated": system}}
		},
		"marketplace/repositories/list": func(map[string]any) any {
			return []any{map[string]any{"id": "123", "full_name": "piitaya/lovelace-mushroom", "name": "Mushroom"}}
		},
	})
}

func marketplaceItems(payloads ...string) string {
	var items []string
	for _, p := range payloads {
		items = append(items, `{"endpoint":"ws","payload":`+p+`}`)
	}
	return "[" + strings.Join(items, ",") + "]"
}

const (
	marketplaceAccept    = `{"type":"marketplace/warning/accept"}`
	marketplaceInstall   = `{"type":"marketplace/repository/install","repository":"123"}`
	marketplaceUninstall = `{"type":"marketplace/repository/uninstall","repository":"123"}`
)

// Accepting the warning for the NOVA App's Supervisor user is a typed plan
// of its own, worded only from confirm_strings: it names that user, that
// every App shares it, and that it can't be withdrawn.
func TestMarketplaceAcceptIsTypedAndSaysWhatItMeans(t *testing.T) {
	paths, sent := marketplaceRelay(t, true)
	exit, env := planRaw(t, paths, marketplaceItems(marketplaceAccept))
	if exit != verbExitConfirmation || env.Tier != "typed" || len(sent()) != 0 {
		t.Fatalf("exit %d tier %s sent %v %+v", exit, env.Tier, sent(), env.Error)
	}
	why := confirmT("en", "raw.why.marketplace_warning")
	if !slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == reasonRawTyped && r.Detail == why }) || !strings.Contains(env.Say, why) {
		t.Fatalf("reasons %+v say %q", env.Reasons, env.Say)
	}
	for _, want := range []string{"Supervisor system user", "all Apps", "no way to withdraw"} {
		if !strings.Contains(why, want) {
			t.Errorf("why %q lacks %q", why, want)
		}
	}
	for _, lang := range []string{"en", "de"} {
		if n := len([]rune(confirmT(lang, "raw.why.marketplace_warning"))); n > phoneWhyLimit {
			t.Errorf("%s: the phone would cut the warning (%d runes)", lang, n)
		}
	}
	var result rawPlanResult
	_ = json.Unmarshal(env.Result, &result)
	if len(result.Items) != 1 || !strings.Contains(result.Items[0].Change, "Marketplace warning for HA NOVA's Home Assistant user") {
		t.Fatalf("items %+v", result.Items)
	}
}

// A person's account, or an account that can't be read, is never accepted
// for: Markus's D2 covers the Supervisor user only.
func TestMarketplaceAcceptOnlyForTheSupervisor(t *testing.T) {
	paths, sent := marketplaceRelay(t, false)
	exit, env := planRaw(t, paths, marketplaceItems(marketplaceAccept))
	if exit != verbExitRefused || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "a person's account") || len(sent()) != 0 {
		t.Fatalf("exit %d %+v sent %v", exit, env.Error, sent())
	}
	unread, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/auth/list": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_TIMEOUT", "timed out"} },
	})
	if exit, env := planRaw(t, unread, marketplaceItems(marketplaceAccept)); exit == verbExitConfirmation || env.Plan != nil {
		t.Fatalf("an unread account planned: exit %d %+v", exit, env)
	}
}

// The acceptance lasts beyond any one install, so it is never one step
// among others: the person approves it on its own.
func TestMarketplaceAcceptStandsAlone(t *testing.T) {
	paths, sent := marketplaceRelay(t, true)
	for _, items := range []string{marketplaceItems(marketplaceAccept, marketplaceInstall), marketplaceItems(marketplaceInstall, `{"message":`+marketplaceAccept+`,"collect_events":{"max_events":1}}`)} {
		exit, env := planRaw(t, paths, items)
		if exit != verbExitRefused || env.Plan != nil || env.Error == nil || env.Error.Code != "PLAN_INVALID" || len(sent()) != 0 {
			t.Fatalf("%s: exit %d %+v", items, exit, env.Error)
		}
	}
}

// Install and uninstall are typed and name the repository.
func TestMarketplaceInstallAndUninstallAreTyped(t *testing.T) {
	paths, sent := marketplaceRelay(t, true)
	exit, env := planRaw(t, paths, marketplaceItems(marketplaceInstall, marketplaceUninstall))
	var result rawPlanResult
	_ = json.Unmarshal(env.Result, &result)
	if exit != verbExitConfirmation || env.Tier != "typed" || len(result.Items) != 2 || len(sent()) != 0 {
		t.Fatalf("exit %d tier %s items %+v %+v", exit, env.Tier, result.Items, env.Error)
	}
	for i, verb := range []string{"install", "uninstall"} {
		if item := result.Items[i]; item.Tier != "typed" || !strings.HasPrefix(strings.ToLower(item.Change), verb+" from the marketplace") || !strings.Contains(item.Change, "piitaya/lovelace-mushroom") {
			t.Errorf("item %d: %+v", i, item)
		}
	}
}

// Only the shapes the skill sends: no version, no replacing a built-in
// integration, no acceptance with fields, always a repository.
func TestMarketplaceRefusesOtherShapes(t *testing.T) {
	for _, body := range []string{
		`{"type":"marketplace/repository/install","repository":"123","version":"v1.0.0"}`,
		`{"type":"marketplace/repository/install","repository":"123","confirm_replace_built_in":true}`,
		`{"type":"marketplace/repository/install","repository":"123","confirm_replace_built_in":false}`,
		`{"type":"marketplace/repository/install"}`,
		`{"type":"marketplace/repository/uninstall","repository":""}`,
		`{"type":"marketplace/warning/accept","user_id":"u-owner"}`,
		`{"type":"marketplace/repository/version","repository":"123","version":"v1.0.0"}`,
		`{"type":"marketplace/repositories/add","repository":"someone/thing","category":"integration"}`,
	} {
		if err := checkRawRelayRequest("ws", []byte(body)); err == nil {
			t.Errorf("%s: not refused", body)
		}
	}
}

// The gate rates the lifecycle as the plans do: typed, also inside a
// collect_events envelope, and refused for a person's account or a write
// the CLI never sends; ha-nova relay refuses it, naming the plan.
func TestMarketplaceGateVerdicts(t *testing.T) {
	supervisor, _ := marketplaceRelay(t, true)
	person, _ := marketplaceRelay(t, false)
	for _, tc := range []struct {
		name  string
		paths runtimePaths
		body  string
		want  gateDecision
	}{
		{"accept", supervisor, marketplaceAccept, gateTyped},
		{"accept in an envelope", supervisor, `{"message":` + marketplaceAccept + `,"collect_events":{"max_events":1}}`, gateTyped},
		{"accept for a person", person, marketplaceAccept, gateRefuse},
		{"install", supervisor, marketplaceInstall, gateTyped},
		{"uninstall", supervisor, marketplaceUninstall, gateTyped},
		{"version pick", supervisor, `{"type":"marketplace/repository/version","repository":"123","version":"v1"}`, gateRefuse},
		{"replace built-in", supervisor, `{"type":"marketplace/repository/install","repository":"123","confirm_replace_built_in":true}`, gateRefuse},
	} {
		paths := tc.paths
		check, err := gateVerdict("ws", []byte(tc.body), func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) })
		if err != nil || check.Verdict != tc.want {
			t.Errorf("%s: %+v %v, want %s", tc.name, check, err, tc.want)
		}
	}
	none := func() (*relaySession, error) { return nil, errors.New("no session") }
	if check, _ := gateVerdict("ws", []byte(marketplaceAccept), none); check.Verdict != gateTyped {
		t.Errorf("accept without a session: %+v", check)
	}
}

// Restarting Home Assistant and a Marketplace update.install, also from a
// script, stay typed, at the gate too.
func TestMarketplaceRestartAndUpdateStayTyped(t *testing.T) {
	h := newWriteHarness(t, marketplaceHA(map[string]any{"warning_accepted": true}))
	for _, args := range [][]string{{"homeassistant.restart"}, {"update.install", "update.card_mod_update"}} {
		env := h.plan(append(args, "--skill", "ha-nova:updates")...)
		if env.Tier != "typed" {
			t.Fatalf("%v: tier %s %+v", args, env.Tier, env.Reasons)
		}
		assertGateParity(t, h.paths, env)
	}
	_, env := h.run(runPlanCommand, "write", "--domain", "script", "--file", draftFile(t, map[string]any{"alias": "Update cards", "sequence": []any{step("update.install", "update.card_mod_update")}}))
	if env.Tier != "typed" {
		t.Fatalf("script: tier %s %+v", env.Tier, env.Reasons)
	}
}

// The Relay's own ask never approves the acceptance together with anything
// else, and an account it can't read at the gate is refused, never left to
// an earlier grant.
func TestMarketplaceGateAskAndUnreadAccount(t *testing.T) {
	paths, _ := marketplaceRelay(t, true)
	ask, err := gatePrepareAsk(paths, "Laptop", []gateAskRequest{{Endpoint: "ws", Body: []byte(marketplaceAccept)}, {Endpoint: "ws", Body: []byte(marketplaceInstall)}})
	if err != nil || ask.Verdict != gateRefuse || ask.Body != "" {
		t.Fatalf("bundled ask: %+v %v", ask, err)
	}
	if ask, err := gatePrepareAsk(paths, "Laptop", []gateAskRequest{{Endpoint: "ws", Body: []byte(marketplaceAccept)}}); err != nil || ask.Verdict != gateTyped || !strings.Contains(ask.Body, "Supervisor") {
		t.Fatalf("alone: %+v %v", ask, err)
	}
	unread, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/auth/list": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_TIMEOUT", "timed out"} },
	})
	check, err := gateVerdict("ws", []byte(marketplaceAccept), func() (*relaySession, error) { return newRelaySession(unread, verbOptions{}) })
	if err != nil || check.Verdict != gateRefuse {
		t.Fatalf("unread account: %+v %v", check, err)
	}
}
