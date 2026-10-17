package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Plan store, hash, and confirmation code (typed-verbs spec §2.1). PR2
// only writes plans; nothing here executes one.

const (
	planTTL           = 15 * time.Minute
	planMaxFiles      = 20
	planSchemaVersion = 3 // 2: no code at rest, channel; 3: channel seal (2026-10-17); apply refuses older plans
)

type planAction struct {
	Service   string         `json:"service"`
	EntityIDs []string       `json:"entity_ids"`
	Data      map[string]any `json:"data"`
	Response  bool           `json:"response,omitempty"` // sent with ?return_response
}

type verbPlan struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	ExpiresAt string `json:"expires_at"`
	Confirm   string `json:"confirm"`
	Code      string `json:"code,omitempty"`
	Channel   string `json:"channel,omitempty"` // typed plans: where the human confirms (chat, form, window)
}

type planDelta struct {
	EntityID string `json:"entity_id"`
	Field    string `json:"field"`
	Before   string `json:"before"`
	After    string `json:"after,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

type storedPlan struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Hash          string `json:"hash"`
	Kind          string `json:"kind"`
	Server        string `json:"server"`
	CreatedAt     string `json:"created_at"`
	ExpiresAt     string `json:"expires_at"`
	Tier          string `json:"tier"`
	Confirm       string `json:"confirm"`
	Channel       string `json:"channel"`                // where the code is typed (confirm_code.go); inside the code's MAC
	ChannelSeal   string `json:"channel_seal,omitempty"` // a typed plan's channelSeal (savePlan): an edited channel or prompt is refused
	Nonce         string `json:"nonce"`
	code          string // a typed plan's code, for its own envelope and say; never stored
	Mismatches    int    `json:"code_mismatches,omitempty"` // apply attempts with a wrong code (3 invalidate)
	// SnapshotFailed: a delete (or dashboard save) whose snapshot failed was
	// handed back; only then may the user choose --no-snapshot. Not in the hash.
	SnapshotFailed bool            `json:"snapshot_failed,omitempty"`
	Prompt         string          `json:"prompt,omitempty"` // a typed plan's code-free confirmation-form text (mcp_confirm.go); not in the hash
	Request        planCallRequest `json:"request"`
	Action         planAction      `json:"action"`
	ConfigRequest  *configRequest  `json:"config_request,omitempty"` // kinds write/delete/revert/restore
	ConfigAction   *configAction   `json:"config_action,omitempty"`
	Reasons        []tierReason    `json:"reasons"`
	Basis          map[string]any  `json:"basis"`
}

// planHash is the nonce-free drift hash over the full operation identity:
// two plans for different targets, services, or data never share it, and
// apply recomputes it to detect drift (spec §2.1).
// Reasons enter it as code+entity+via only: free-text details (counts,
// names) would turn every harmless fluctuation into drift.
func planHash(kind, server string, action any, tier string, reasons []tierReason, basis map[string]any) string {
	type reasonKey struct{ Code, EntityID, Via string }
	keys := []reasonKey{}
	for _, r := range reasons {
		if key := (reasonKey{r.Code, r.EntityID, r.Via}); !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	canonical, _ := json.Marshal(struct {
		Kind    string         `json:"kind"`
		Server  string         `json:"server"`
		Action  any            `json:"action"`
		Tier    string         `json:"tier"`
		Reasons []reasonKey    `json:"reasons"`
		Basis   map[string]any `json:"basis"`
	}{kind, server, action, tier, keys, basis})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// buildCallPlan freezes the action, hashes it, and fills the envelope; the
// caller decides whether the plan is stored.
func buildCallPlan(e *tierEngine, env *verbEnvelope, req planCallRequest, targets []verbTarget) (storedPlan, error) {
	frozen := []string{}
	for _, t := range targets {
		frozen = append(frozen, t.EntityID)
	}
	action := planAction{Service: req.Service, EntityIDs: frozen, Data: req.Data, Response: req.wantsResponse()}
	plan := storedPlan{Kind: "call", Request: req, Action: action, Basis: callPlanBasis(e, req.Service, frozen, req.Data)}
	plan, err := sealPlan(e, env, plan, action)
	env.Action = &action
	env.Deltas = planDeltas(req.Service, frozen, req.Data, e.states)
	if env.Status == "needs_confirmation" {
		env.Say, plan.Prompt = confirmationSay(e, env, plan, ""), confirmationPrompt(e, env, plan, "")
	}
	return plan, err
}

// sealPlan hashes the action (nonce-free drift hash), draws the nonce, and
// derives id and code; it fills the envelope fields every plan kind shares.
func sealPlan(e *tierEngine, env *verbEnvelope, plan storedPlan, action any) (storedPlan, error) {
	key := func(r tierReason) string {
		return strings.Join([]string{r.Code, r.EntityID, r.Via, r.Detail, r.Skill}, "|")
	}
	sort.SliceStable(e.reasons, func(i, j int) bool { return key(e.reasons[i]) < key(e.reasons[j]) })
	tier := e.level.String()
	sum := planHash(plan.Kind, env.Server, action, tier, e.reasons, plan.Basis)
	// The per-plan nonce makes every id and code unique, so a re-plan never
	// overwrites a plan or replays an earlier code; the drift hash stays
	// nonce-free.
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return storedPlan{}, fmt.Errorf("cannot draw a plan nonce: %w", err)
	}
	idHex := planIdentity(sum, hex.EncodeToString(nonce))
	now := verbNow().UTC()
	plan.SchemaVersion, plan.ID, plan.Hash, plan.Server, plan.Nonce = planSchemaVersion, "p_"+idHex[:12], "sha256:"+sum, env.Server, hex.EncodeToString(nonce)
	plan.CreatedAt, plan.ExpiresAt = now.Format(time.RFC3339), now.Add(planTTL).Format(time.RFC3339)
	plan.Tier, plan.Reasons, plan.Channel = tier, e.reasons, planChannel
	plan.Confirm = map[tierLevel]string{tierOrdinary: "none", tierPreview: "natural", tierTyped: "code"}[e.level]
	shownChannel := ""
	if e.level == tierTyped {
		if e.s.transport.Credential == "" { // unreachable: a session always holds one
			return storedPlan{}, errors.New("cannot derive the one-time code: no Relay credential")
		}
		if plan.Channel == "" {
			plan.Channel = detectChannel(e.s)
		}
		if plan.Channel == channelPhoneMissing {
			return storedPlan{}, refusedWith("CONFIRM_UNAVAILABLE", "this change is confirmed on the Home Assistant owner's phone, and HA NOVA can't reach it now", phoneMissingNext)
		}
		if !humanChannel(plan.Channel) { // a window or phone plan's code never leaves this process
			plan.code = confirmCode(e.s.transport.Credential, idHex, plan.Channel, plan.ExpiresAt)
		}
		shownChannel = plan.Channel
	}
	if where := map[string]string{channelWindow: "a confirmation window on their computer", channelPhone: "a notification on their phone"}[shownChannel]; where != "" {
		env.NextStep = fmt.Sprintf("Show `say`. When the user has asked for this change and your workflow has reached this step (a skill's order comes first), run ha-nova apply --plan %s without --confirm and wait for its result (give the command up to 2 minutes): %s asks them. Never ask for a code in chat.", plan.ID, where)
	}
	if plan.Channel == "" { // no code, so where it would be typed does not matter
		plan.Channel = channelChat
	}
	env.Tier, env.Reasons = tier, e.reasons
	env.Members, env.Consumers = e.shown()
	switch {
	case len(env.Consumers) > 0:
	case plan.Kind == "delete" && plan.ConfigAction != nil && plan.ConfigAction.Domain != "dashboard" && e.complete:
		env.Consumers = []tierConsumer{} // referencedBy scanned it all and found none: say so
	default:
		env.Consumers = nil // omitted: no complete delete scan
	}
	env.Coverage = &tierCoverage{Complete: e.complete, Notes: e.notes, Skipped: e.skipped}
	env.Plan = &verbPlan{ID: plan.ID, Hash: plan.Hash, ExpiresAt: plan.ExpiresAt, Confirm: plan.Confirm, Code: plan.code, Channel: shownChannel}
	env.Status = "ready"
	if e.level != tierOrdinary {
		env.Status, env.exit = "needs_confirmation", verbExitConfirmation
	}
	return plan, nil
}

// callPlanBasis: per acted entity pl/device_class/supported_features, the
// before value of every field the call changes on the frozen targets (the
// state only when the service changes it), and the normalized config
// digests (spec §2.1). Listener surpluses already show in tier and reasons.
func callPlanBasis(e *tierEngine, service string, frozen []string, data map[string]any) map[string]any {
	_, name := splitID(service)
	changesState := serviceAfterState[name] != "" || name == "toggle" || strings.HasPrefix(name, "alarm_arm_")
	entities := map[string]any{}
	for _, id := range e.acted {
		st := e.states[id]
		entities[id] = map[string]any{"pl": e.reg[id].pl, "device_class": st.attr("device_class"), "supported_features": st.attr("supported_features")}
	}
	before := map[string]any{}
	for _, id := range frozen {
		st := e.states[id]
		fields := map[string]any{}
		if changesState {
			fields["state"] = st.state()
		}
		for key := range data {
			fields[key] = beforeValue(id, key, st)
		}
		before[id] = fields
	}
	return map[string]any{"entities": entities, "before": before, "configs": e.basis}
}

// planIdentity is hex(sha256(hash ‖ nonce)): the plan id and the code's MAC
// input (confirm_code.go), recomputable at apply from the recomputed hash and stored nonce.
func planIdentity(sum, nonceHex string) string {
	idSum := sha256.Sum256([]byte(sum + nonceHex))
	return hex.EncodeToString(idSum[:])
}

// savePlan writes CacheDir/plans/<id>.json (0600, atomic) after pruning
// expired files and keeping at most planMaxFiles plans.
func savePlan(paths runtimePaths, plan storedPlan, credential string) error {
	if plan.Tier == tierTyped.String() {
		plan.ChannelSeal = channelSeal(credential, plan) // after Prompt is final
	}
	dir := planDir(paths)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot store the plan: %w", err)
	}
	prunePlans(dir)
	return writePlanFile(filepath.Join(dir, plan.ID+".json"), plan)
}

func planDir(paths runtimePaths) string { return filepath.Join(paths.CacheDir, "plans") }

func writePlanFile(path string, plan storedPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err == nil {
		err = writeFileAtomic(path, append(data, '\n'), 0o600)
	}
	if err != nil {
		return fmt.Errorf("cannot store the plan: %w", err)
	}
	return nil
}

func readPlanFile(path string) (storedPlan, error) {
	var plan storedPlan
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &plan)
	}
	return plan, err
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
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if verbNow().Sub(info.ModTime()) > planTTL { // plans, stale temp files, abandoned claims
			_ = os.Remove(path)
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
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
// unavailable/unknown warnings. This is the rule's source since the
// service-call skill moved to `ha-nova call` (formerly its state-delta rules).
func planDeltas(service string, frozen []string, data map[string]any, states map[string]*entityState) []planDelta {
	var deltas []planDelta
	for _, id := range frozen {
		st := states[id]
		if st == nil {
			continue
		}
		// The state a target ends in follows the service it really gets
		// (homeassistant.turn_on on a cover opens it); scenes and scripts
		// have no on/off end state.
		domain, _ := splitID(id)
		_, name := splitID(effectiveService(service, domain))
		after := serviceAfterState[name]
		if strings.HasPrefix(name, "alarm_arm_") {
			after = "armed_" + strings.TrimPrefix(name, "alarm_arm_")
		}
		if domain == "scene" || domain == "script" || (name == "turn_on" || name == "turn_off") && !onOffDomains[domain] {
			after = ""
		}
		warning := map[string]string{"unavailable": "Device is offline or unreachable.", "unknown": "State not yet known; the call may still work."}[st.State]
		if after != "" || warning != "" {
			deltas = append(deltas, planDelta{EntityID: id, Field: "state", Before: st.State, After: after, Warning: warning})
		}
		for _, key := range sortedKeys(data) {
			d := planDelta{EntityID: id, Field: key, Before: deltaValue(beforeValue(id, key, st)), After: deltaValue(data[key])}
			switch key {
			case "brightness_pct":
				d.Field, d.Before, d.After = "brightness", brightnessPct(st), d.After+"%"
			case "brightness":
				d.Before, d.After = brightnessPct(st), brightnessPct(&entityState{State: "on", Attributes: map[string]any{"brightness": data[key]}})
			case "position", "tilt_position":
				d.Before, d.After = withPercent(d.Before), withPercent(d.After)
			}
			deltas = append(deltas, d)
		}
	}
	return deltas
}

// stateBackedField: a service field whose current value IS the entity state
// (select option, number/text value, climate hvac_mode). Verification, the
// drift basis, and deltas all read it from st.State.
func stateBackedField(domain, key string) bool {
	switch key {
	case "option":
		return domain == "select" || domain == "input_select"
	case "value":
		return domain == "number" || domain == "input_number" || domain == "text" || domain == "input_text"
	case "hvac_mode":
		return domain == "climate"
	}
	return false
}

// beforeValue is the current value of the field a call sets on entityID.
func beforeValue(entityID, key string, st *entityState) any {
	if domain, _ := splitID(entityID); stateBackedField(domain, key) {
		if st == nil {
			return nil
		}
		return st.State
	}
	return st.attr(deltaAttribute(key))
}

// deltaAttribute names the state attribute a service field changes.
func deltaAttribute(field string) string {
	switch field {
	case "brightness_pct":
		return "brightness"
	case "position":
		return "current_position"
	case "tilt_position":
		return "current_tilt_position"
	}
	return field
}

// brightnessPct shows HA's 0–255 as %; a light that is off counts as 0%,
// a missing value on a light that is on is "unknown".
func brightnessPct(st *entityState) string {
	n, isNumber := st.attr("brightness").(float64)
	switch {
	case isNumber:
		return fmt.Sprintf("%d%%", int(math.Round(n/255*100)))
	case st != nil && st.State == "off":
		return "0%"
	}
	return "unknown"
}

func withPercent(v string) string {
	if v == "" {
		return "unknown"
	}
	return v + "%"
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
