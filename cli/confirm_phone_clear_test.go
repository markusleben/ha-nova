package main

import (
	"os"
	"strings"
	"testing"
)

// One HA NOVA notification on the phone, never a group: a new question
// clears the last result there first. Android stacked the ten-minute
// "Linked to HA NOVA" and the next question into "Home Assistant (2)",
// which had to be opened before the question. A clear never names an open
// question's tag, and carries no ids.
func TestNewQuestionClearsTheLastResult(t *testing.T) {
	h, p, _ := familyLink(t)
	var order []string
	notify := phoneNotify
	phoneNotify = func(s *relaySession, service string, body map[string]any) error {
		tag := body["data"].(map[string]any)["tag"].(string)
		_, ask := body["data"].(map[string]any)["actions"]
		switch {
		case body["message"] == "clear_notification":
			if data := body["data"].(map[string]any); len(body) != 2 || len(data) != 2 || data["priority"] != "high" {
				t.Errorf("clear carries more than its tag: %v", body)
			}
			order = append(order, "clear "+tag)
		case ask:
			order = append(order, "ask "+tag)
		default:
			order = append(order, "result "+tag)
		}
		return notify(s, service, body)
	}
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"}); exit != 0 {
		t.Fatalf("link: %d %q", exit, out)
	}
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || env.Status != "verified" {
		t.Fatalf("apply: %d %+v", exit, env.Error)
	}
	if len(order) != 5 {
		t.Fatalf("order %v", order)
	}
	link, unlock := strings.Fields(order[0])[1], strings.Fields(order[3])[1]
	want := []string{"ask " + link, "result " + link, "clear " + link, "ask " + unlock, "result " + unlock}
	if strings.Join(order, ",") != strings.Join(want, ",") || link == unlock {
		t.Fatalf("order %v, want %v", order, want)
	}
	if got := phoneLastResults(h.paths); len(got) != 1 || got["owner-id mobile_app_annas_pixel"] != unlock {
		t.Fatalf("kept %v", got)
	}
	for _, path := range managedCacheArtifactPaths(h.paths) {
		if path == phoneResultsFile(h.paths) {
			return // uninstall removes it
		}
	}
	t.Fatal("uninstall leaves the phone results file behind")
}

// Two homes can both have mobile_app_pixel: each clears only its own result.
func TestPhoneResultsArePerHome(t *testing.T) {
	paths := runtimePaths{CacheDir: t.TempDir()}
	notePhoneResult(paths, "home-a", []string{"mobile_app_pixel"}, "tag-a")
	notePhoneResult(paths, "home-b", []string{"mobile_app_pixel"}, "tag-b")
	var cleared []string
	notify := func(_ *relaySession, _ string, body map[string]any) error {
		if body["message"] == "clear_notification" {
			cleared = append(cleared, body["data"].(map[string]any)["tag"].(string))
		}
		return nil
	}
	_ = clearingFirst(phoneLastResults(paths), "home-a", notify)(nil, "mobile_app_pixel", map[string]any{})
	if strings.Join(cleared, ",") != "tag-a" {
		t.Fatalf("cleared %v", cleared)
	}
	// A file holding null reads as empty, and a result can still be kept.
	if err := os.WriteFile(phoneResultsFile(paths), []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	notePhoneResult(paths, "home-a", []string{"mobile_app_pixel"}, "tag-c")
	if got := phoneLastResults(paths); got["home-a mobile_app_pixel"] != "tag-c" {
		t.Fatalf("after null: %v", got)
	}
}
