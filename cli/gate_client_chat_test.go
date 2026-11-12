package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A chat bridge outside the App (the standalone container, the hosted demo)
// is gated like any device. It asks the container's phone only from its
// apply job (mcp_serve_relay_ask_test.go); a chat call outside one (ha_call)
// gets the hold as its own refusal, sent once and never retried. The App's
// bridge gets the hold as it is, and a computer's same hold still asks.
func TestGateChatBridgeHoldRefuses(t *testing.T) {
	body := []byte(`{"body":{"entity_id":["light.kitchen"]},"method":"POST","path":"/api/services/light/turn_on"}`)
	for _, who := range []string{"chat bridge", "App chat bridge", "computer"} {
		t.Run(who, func(t *testing.T) {
			g := &fakeGate{hold: func(send gateSend) any {
				if send.grant == "" {
					return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "Only a person may confirm this"}
				}
				return nil
			}}
			h := gatedHarness(t, newFakeHA(), g)
			s, err := newRelaySession(h.paths, verbOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if who != "computer" {
				saved := chatCall
				chatCall = &chatPolicy{codes: true}
				t.Cleanup(func() { chatCall = saved })
			}
			if who == "App chat bridge" {
				s.cfg.bridgeCredential = "test-token"
			}
			_, err = s.sendWrite("core", body)
			_, ops, sends := g.snapshot()
			var failure *verbFailure
			var call *relayCallError
			switch who {
			case "chat bridge":
				if !errors.As(err, &failure) || failure.code != "CONFIRM_UNAVAILABLE" || failure.message != confirmT("en", "gate.chat_held") || failure.next != gateChatHeldNext ||
					!errors.Is(err, errNothingSent) || len(sends) != 1 || len(ops) != 0 {
					t.Fatalf("err %v ops %v sends %+v", err, ops, sends)
				}
			case "App chat bridge":
				if errors.As(err, &failure) || !errors.As(err, &call) || call.status != 428 || call.code != "GATE_APPROVAL_REQUIRED" || len(sends) != 1 || len(ops) != 0 {
					t.Fatalf("err %v ops %v sends %+v", err, ops, sends)
				}
			default:
				if err != nil || len(ops) == 0 || ops[0] != "open" || len(sends) != 2 || sends[1].grant != "grant-ask1" {
					t.Fatalf("err %v ops %v sends %+v", err, ops, sends)
				}
			}
		})
	}
}

// A chat app's file and helper changes are raw plans: the Relay's hold stops
// the step unsent, with the refusal's words and what to tell the user, not
// an invitation to plan it again.
func TestGateChatBridgeHoldInRawPlan(t *testing.T) {
	saved := chatCall
	chatCall = &chatPolicy{codes: true, level: levelManage}
	t.Cleanup(func() { chatCall = saved })
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"input_boolean/create": func(map[string]any) any {
			return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "Only a person may confirm this"}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"Guest mode"}}]`)
	_, env = applyRaw(t, paths, env)
	if env.Error == nil || env.Error.Code != "RAW_FAILED" || !strings.Contains(env.Error.Message, confirmT("en", "gate.chat_held")) || env.Error.Next != gateChatHeldNext ||
		statuses(rawOutcomes(t, env)) != "not_sent" || strings.Join(sent(), " | ") != "input_boolean/create" {
		t.Fatalf("%+v %s sent %v", env.Error, env.Result, sent())
	}
}

// After an earlier step went through, the held step's Next keeps that step
// in the report instead of saying nothing changed.
func TestGateChatBridgeHoldInRawPlanAfterADoneStep(t *testing.T) {
	saved := chatCall
	chatCall = &chatPolicy{codes: true, level: levelManage}
	t.Cleanup(func() { chatCall = saved })
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"input_number/create": func(map[string]any) any {
			return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "Only a person may confirm this"}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"Guest mode"}},{"endpoint":"ws","payload":{"type":"input_number/create","name":"Level","min":0,"max":10}}]`)
	_, env = applyRaw(t, paths, env)
	if env.Error == nil || env.Error.Code != "RAW_FAILED" || env.Error.Next != gateChatHeldPartNext ||
		!strings.Contains(env.Error.Message, "1 earlier step went through") || strings.Join(sent(), " | ") != "input_boolean/create | input_number/create" {
		t.Fatalf("%+v %s sent %v", env.Error, env.Result, sent())
	}
}

// On the container a chat app's raw plan typed only by a skill's mark keeps
// the bridge's own ask (the Relay neither sees nor holds the mark): a chat
// code at Manage everything, never the container's phone; one typed by its
// own request asks the container's phone.
func TestGateChatContainerKeepsLocalAsks(t *testing.T) {
	saved := chatCall
	chatCall = &chatPolicy{codes: true, level: levelManage}
	t.Cleanup(func() { chatCall = saved })
	g := &fakeGate{}
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"health <nil>":          func(map[string]any) any { return map[string]any{"status": "ok", "gate": 1} },
		gateAskRoute + " <nil>": func(p map[string]any) any { return g.ask(context.Background(), p) },
		"input_boolean/list": func(map[string]any) any {
			return []any{map[string]any{"id": "guest", "name": "Guest"}}
		},
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "input_boolean.guest", "platform": "input_boolean", "unique_id": "guest"}}
		},
	})
	_, marked := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/update","input_boolean_id":"guest","name":"G"},"typed":true}]`)
	if marked.Tier != "typed" || marked.Plan == nil || marked.Plan.Channel != channelChat || marked.Plan.Code == "" {
		t.Fatalf("skill-typed plan: %+v", marked)
	}
	_, typed := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	if typed.Tier != "typed" || typed.Plan == nil || typed.Plan.Channel != channelRelay || typed.Plan.Code != "" || !strings.Contains(typed.Say, confirmT("en", "say.relay_ask_container")) {
		t.Fatalf("typed plan: %+v", typed)
	}
}
