package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanHashCoversOperationIdentity(t *testing.T) {
	f := newFakeHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	plan := func(args ...string) verbPlan {
		t.Helper()
		exit, env := runVerbForTest(t, func() int { return runPlanCommand(paths, append([]string{"call"}, args...)) })
		if exit != 0 || env.Plan == nil {
			t.Fatalf("%v: exit %d env %+v", args, exit, env)
		}
		return *env.Plan
	}
	base := plan("light.turn_on", "light.kitchen")
	if again := plan("light.turn_on", "light.kitchen"); again.ID != base.ID || again.Hash != base.Hash {
		t.Fatalf("same request, different plan: %+v vs %+v", base, again)
	}
	seen := map[string]string{base.ID: "base"}
	for name, args := range map[string][]string{
		"service": {"light.turn_off", "light.kitchen"},
		"target":  {"light.turn_on", "light.hall"},
		"data":    {"light.turn_on", "light.kitchen", "--set", "brightness_pct=40"},
	} {
		p := plan(args...)
		if other, clash := seen[p.ID]; clash {
			t.Fatalf("%s change kept plan id %s (same as %s)", name, p.ID, other)
		}
		seen[p.ID] = name
	}
}

func TestPlanFileIsPrivateAndBindsTheRequest(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.front_door", "zwave_js", "locked", nil)
	exit, env, paths, log := runPlanForTest(t, f, "lock.unlock", "lock.front_door")
	if exit != verbExitConfirmation || env.Status != "needs_confirmation" {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	path := filepath.Join(paths.CacheDir, "plans", env.Plan.ID+".json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("plan file %s: %v mode %v", path, err, info)
	}
	raw, _ := os.ReadFile(path)
	var stored storedPlan
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Kind != "call" || stored.Server != env.Server || stored.Code != env.Plan.Code || stored.Action.Service != "lock.unlock" ||
		len(stored.Action.EntityIDs) != 1 || stored.Request.EntityIDs[0] != "lock.front_door" || "sha256:"+planHash(stored.Kind, stored.Server, stored.Action, stored.Tier, stored.Reasons, stored.Basis) != stored.Hash {
		t.Fatalf("stored plan does not bind the request: %+v", stored)
	}
	for _, c := range log.all() { // the fake relay's I1 oracle already refuses writes
		if c.endpoint == "core" && c.payload["method"] != "GET" {
			t.Fatalf("plan sent %v", c.payload)
		}
	}
}

func TestPlanStorePrunesExpiredAndBoundsCount(t *testing.T) {
	paths := runtimePaths{CacheDir: t.TempDir()}
	dir := filepath.Join(paths.CacheDir, "plans")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 25; i++ {
		path := filepath.Join(dir, fmt.Sprintf("p_fresh%02d.json", i))
		_ = os.WriteFile(path, []byte("{}"), 0o600)
		_ = os.Chtimes(path, now, now.Add(time.Duration(i-30)*time.Second))
	}
	expired := filepath.Join(dir, "p_expired.json")
	_ = os.WriteFile(expired, []byte("{}"), 0o600)
	_ = os.Chtimes(expired, now, now.Add(-planTTL-time.Minute))
	if err := savePlan(paths, storedPlan{ID: "p_new"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != planMaxFiles {
		t.Fatalf("%d plan files after prune, want %d", len(entries), planMaxFiles)
	}
	for _, gone := range []string{expired, filepath.Join(dir, "p_fresh00.json")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s survived the prune", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "p_new.json")); err != nil {
		t.Fatalf("new plan missing: %v", err)
	}
}
