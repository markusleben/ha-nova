package main

import (
	"fmt"
	"strings"
	"testing"
)

// Owned services left the raw routes (2026-10-17): from their owning skill
// (--skill) they are planned and tiered like any call; without it, or from
// another skill, they stay refused before any Relay contact.
func TestOwnedServicesRunFromTheirSkill(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		exit       int
		tier, code string
	}{
		{[]string{"notify.notify", "--skill", "ha-nova:notify", "--set", "message=hi"}, 3, "preview", reasonOwnedMember},
		{[]string{"todo.add_item", "todo.shop", "--skill", "ha-nova:todo", "--set", "item=milk"}, 3, "preview", reasonOwnedMember},
		{[]string{"mqtt.publish", "--skill", "ha-nova:mqtt", "--set", "topic=a/b", "--set", "payload=x"}, 3, "typed", reasonOwnedTypedMember},
		{[]string{"conversation.process", "--skill", "ha-nova:assist", "--set", "text=open the garage"}, 3, "typed", reasonOwnedTypedMember},
		{[]string{"hassio.host_reboot", "--skill", "ha-nova:service-call"}, 3, "typed", reasonOwnedTypedMember},
		{[]string{"mqtt.publish", "--set", "topic=a/b"}, 4, "refused", reasonOwnedService},
		{[]string{"mqtt.publish", "--skill", "ha-nova:notify", "--set", "topic=a/b"}, 4, "refused", reasonOwnedService},
		{[]string{"hassio.host_reboot"}, 4, "refused", reasonHassio},
	} {
		f := newFakeHA()
		f.ent("todo.shop", "local_todo", "1", nil)
		exit, env, _, log := runPlanForTest(t, f, tc.args...)
		if exit != tc.exit || env.Tier != tc.tier || len(env.Reasons) == 0 || env.Reasons[0].Code != tc.code {
			t.Errorf("%v: exit %d tier %q reasons %+v", tc.args, exit, env.Tier, env.Reasons)
		}
		if tc.exit == 4 && len(log.all()) != 0 {
			t.Errorf("%v: a static refusal contacted the Relay", tc.args)
		}
		if strings.Contains(env.Say, "This also") {
			t.Errorf("%v: the call itself reads as a member: %q", tc.args, env.Say)
		}
	}
}

// --set types a value only when the JSON round-trip is lossless: a version
// such as 1.10 must reach Home Assistant as typed, never as 1.1.
func TestSetValueKeepsLossyNumbersAsStrings(t *testing.T) {
	for in, want := range map[string]any{
		"40": float64(40), "true": true, "null": nil, `"x"`: "x", "[1,2]": []any{float64(1), float64(2)},
		"1.10": "1.10", "1e3": "1e3", "007": "007", "2026.10.1": "2026.10.1", "1.0": "1.0", "abc": "abc", " 4": " 4", "[255, 0, 0]": []any{float64(255), float64(0), float64(0)},
	} {
		got := setValue(in)
		if fmt.Sprint(got) != fmt.Sprint(want) || fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Errorf("setValue(%q) = %#v, want %#v", in, got, want)
		}
	}
}
