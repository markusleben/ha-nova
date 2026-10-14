package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Plan store, hash, and confirmation code (typed-verbs spec §2.1). PR2
// only writes plans; nothing here executes one.

const (
	planTTL      = 15 * time.Minute
	planMaxFiles = 20
)

type planAction struct {
	Service   string         `json:"service"`
	EntityIDs []string       `json:"entity_ids"`
	Data      map[string]any `json:"data"`
}

type verbPlan struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	ExpiresAt string `json:"expires_at"`
	Confirm   string `json:"confirm"`
	Code      string `json:"code,omitempty"`
}

type planDelta struct {
	EntityID string `json:"entity_id"`
	Field    string `json:"field"`
	Before   string `json:"before"`
	After    string `json:"after,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

type storedPlan struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Hash          string          `json:"hash"`
	Kind          string          `json:"kind"`
	Server        string          `json:"server"`
	CreatedAt     string          `json:"created_at"`
	ExpiresAt     string          `json:"expires_at"`
	Tier          string          `json:"tier"`
	Confirm       string          `json:"confirm"`
	Code          string          `json:"code,omitempty"`
	Request       planCallRequest `json:"request"`
	Action        planAction      `json:"action"`
	Reasons       []tierReason    `json:"reasons"`
	Basis         map[string]any  `json:"basis"`
}

// planHash covers the full operation identity: two plans for different
// targets, services, or data never share a hash or an id.
func planHash(kind, server string, action planAction, tier string, reasons []tierReason, basis map[string]any) string {
	canonical, _ := json.Marshal(struct {
		Kind    string         `json:"kind"`
		Server  string         `json:"server"`
		Action  planAction     `json:"action"`
		Tier    string         `json:"tier"`
		Reasons []tierReason   `json:"reasons"`
		Basis   map[string]any `json:"basis"`
	}{kind, server, action, tier, reasons, basis})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func storeCallPlan(paths runtimePaths, e *tierEngine, env *verbEnvelope, req planCallRequest, targets []verbTarget) error {
	frozen := []string{}
	for _, t := range targets {
		frozen = append(frozen, t.EntityID)
	}
	sort.SliceStable(e.reasons, func(i, j int) bool {
		a, b := e.reasons[i], e.reasons[j]
		return a.Code+a.EntityID+a.Via+a.Detail < b.Code+b.EntityID+b.Via+b.Detail
	})
	action := planAction{Service: req.Service, EntityIDs: frozen, Data: req.Data}
	tier := e.level.String()
	basis := callPlanBasis(e, frozen, req.Data)
	sum := planHash("call", env.Server, action, tier, e.reasons, basis)
	now := verbNow().UTC()
	plan := storedPlan{
		SchemaVersion: 1, ID: "p_" + sum[:12], Hash: "sha256:" + sum, Kind: "call", Server: env.Server,
		CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(planTTL).Format(time.RFC3339),
		Tier: tier, Confirm: map[tierLevel]string{tierOrdinary: "none", tierPreview: "natural", tierTyped: "code"}[e.level],
		Request: req, Action: action, Reasons: e.reasons, Basis: basis,
	}
	if e.level == tierTyped {
		plan.Code = confirmCode(req.Service, frozen, sum)
	}
	if err := savePlan(paths, plan); err != nil {
		return fmt.Errorf("cannot store the plan: %w", err)
	}
	env.Tier, env.Action, env.Reasons = tier, &action, e.reasons
	env.Members, env.Consumers = e.members, e.consumers
	env.Coverage = &tierCoverage{Complete: e.complete, Notes: e.notes}
	env.Deltas = planDeltas(req.Service, frozen, req.Data, e.states)
	env.Plan = &verbPlan{ID: plan.ID, Hash: plan.Hash, ExpiresAt: plan.ExpiresAt, Confirm: plan.Confirm, Code: plan.Code}
	env.Status = "ready"
	if e.level != tierOrdinary {
		env.Status, env.exit = "needs_confirmation", verbExitConfirmation
	}
	return nil
}

// callPlanBasis: per acted entity pl/device_class/supported_features, the
// before value of every changed field on the frozen targets, the normalized
// config digests, and the call_service listener count (spec §2.1).
func callPlanBasis(e *tierEngine, frozen []string, data map[string]any) map[string]any {
	entities := map[string]any{}
	for _, id := range e.acted {
		st := e.states[id]
		entities[id] = map[string]any{"pl": e.reg[id].pl, "device_class": st.attr("device_class"), "supported_features": st.attr("supported_features")}
	}
	before := map[string]any{}
	for _, id := range frozen {
		st := e.states[id]
		fields := map[string]any{"state": nil}
		if st != nil {
			fields["state"] = st.State
		}
		for key := range data {
			fields[key] = st.attr(strings.TrimSuffix(key, "_pct"))
		}
		before[id] = fields
	}
	return map[string]any{"entities": entities, "before": before, "configs": e.basis, "call_service_listeners": e.events["call_service"]}
}

func confirmCode(service string, frozen []string, sum string) string {
	_, verb := splitID(service)
	object := verb
	if len(frozen) > 0 {
		_, object = splitID(frozen[0])
	}
	if len(object) > 24 {
		object = object[:24]
	}
	return "confirm:" + verb + "-" + object + "-" + sum[:6]
}

// savePlan writes CacheDir/plans/<id>.json (0600, atomic) after pruning
// expired plans and keeping at most planMaxFiles.
func savePlan(paths runtimePaths, plan storedPlan) error {
	dir := filepath.Join(paths.CacheDir, "plans")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	prunePlans(dir)
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, plan.ID+".json"), append(data, '\n'), 0o600)
}

func prunePlans(dir string) {
	entries, _ := os.ReadDir(dir)
	type planFile struct {
		path string
		mod  time.Time
	}
	var keep []planFile
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !strings.HasSuffix(entry.Name(), ".json") || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if verbNow().Sub(info.ModTime()) > planTTL {
			_ = os.Remove(path)
			continue
		}
		keep = append(keep, planFile{path, info.ModTime()})
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].mod.Before(keep[j].mod) })
	for len(keep) >= planMaxFiles {
		_ = os.Remove(keep[0].path)
		keep = keep[1:]
	}
}

var serviceAfterState = map[string]string{
	"turn_on": "on", "turn_off": "off", "lock": "locked", "unlock": "unlocked", "open": "open",
	"open_cover": "open", "close_cover": "closed", "alarm_disarm": "disarmed", "alarm_trigger": "triggered",
}

// planDeltas renders before → after per frozen target: brightness in %,
// unavailable/unknown warnings (service-call/SKILL.md state-delta rules).
func planDeltas(service string, frozen []string, data map[string]any, states map[string]*entityState) []planDelta {
	_, name := splitID(service)
	after := serviceAfterState[name]
	if strings.HasPrefix(name, "alarm_arm_") {
		after = "armed_" + strings.TrimPrefix(name, "alarm_arm_")
	}
	var deltas []planDelta
	for _, id := range frozen {
		st := states[id]
		if st == nil {
			continue
		}
		warning := map[string]string{"unavailable": "Device is offline or unreachable.", "unknown": "State not yet known; the call may still work."}[st.State]
		if after != "" || warning != "" {
			deltas = append(deltas, planDelta{EntityID: id, Field: "state", Before: st.State, After: after, Warning: warning})
		}
		for _, key := range sortedKeys(data) {
			d := planDelta{EntityID: id, Field: key, Before: deltaValue(st.attr(key)), After: deltaValue(data[key])}
			switch key {
			case "brightness_pct":
				d.Field, d.Before, d.After = "brightness", brightnessPct(st.attr("brightness")), d.After+"%"
			case "brightness":
				d.Before, d.After = brightnessPct(st.attr("brightness")), brightnessPct(data[key])
			}
			deltas = append(deltas, d)
		}
	}
	return deltas
}

// brightnessPct shows HA's 0–255 as %; a light that is off counts as 0%.
func brightnessPct(v any) string {
	n, _ := v.(float64)
	return fmt.Sprintf("%d%%", int(math.Round(n/255*100)))
}

func deltaValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
