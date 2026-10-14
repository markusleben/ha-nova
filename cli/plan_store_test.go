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
	// Same request: same nonce-free drift hash, but a fresh id (per-plan
	// nonce), so a re-plan never overwrites a plan or replays its code.
	again := plan("light.turn_on", "light.kitchen")
	if again.Hash != base.Hash || again.ID == base.ID {
		t.Fatalf("re-plan: %+v vs %+v", base, again)
	}
	for _, id := range []string{base.ID, again.ID} {
		if _, err := os.Stat(filepath.Join(paths.CacheDir, "plans", id+".json")); err != nil {
			t.Fatalf("plan %s overwritten or missing: %v", id, err)
		}
	}
	seen := map[string]string{base.Hash: "base"}
	for name, args := range map[string][]string{
		"service": {"light.turn_off", "light.kitchen"},
		"target":  {"light.turn_on", "light.hall"},
		"data":    {"light.turn_on", "light.kitchen", "--set", "brightness_pct=40"},
	} {
		p := plan(args...)
		if other, clash := seen[p.Hash]; clash {
			t.Fatalf("%s change kept hash %s (same as %s)", name, p.Hash, other)
		}
		seen[p.Hash] = name
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
	var stale []string
	for _, name := range []string{"p_expired.json", "p_x.json.tmp-123", "p_y.claimed-42"} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, []byte("{}"), 0o600)
		_ = os.Chtimes(path, now, now.Add(-planTTL-time.Minute))
		stale = append(stale, path)
	}
	freshClaim := filepath.Join(dir, "p_z.claimed-7")
	_ = os.WriteFile(freshClaim, []byte("{}"), 0o600)
	if err := savePlan(paths, storedPlan{ID: "p_new"}); err != nil {
		t.Fatal(err)
	}
	plans, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(plans) != planMaxFiles {
		t.Fatalf("%d plan files after prune, want %d", len(plans), planMaxFiles)
	}
	if _, err := os.Stat(freshClaim); err != nil {
		t.Fatalf("a fresh claim was pruned: %v", err)
	}
	for _, gone := range append(stale, filepath.Join(dir, "p_fresh00.json")) {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s survived the prune", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "p_new.json")); err != nil {
		t.Fatalf("new plan missing: %v", err)
	}
}
