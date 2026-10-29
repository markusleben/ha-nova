package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	planMaxFiles      = 50
	planSchemaVersion = 5 // 2: no code at rest, channel; 3: channel seal (2026-10-17); 4: language in the seal; 5: Home Assistant words in the seal (2026-10-29); apply refuses older plans
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
	Channel   string `json:"channel,omitempty"` // typed plans: where the human confirms (chat, form, window, phone); after apply: where they did (window, phone)
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
	Lang           string          `json:"lang,omitempty"`   // the confirmation catalog its texts use (confirm_strings.go); sealed when typed, not in the hash
	Words          haWords         `json:"words,omitempty"`  // Home Assistant's names its texts use, in Lang (confirm_words.go); likewise
	Request        planCallRequest `json:"request"`
	Action         planAction      `json:"action"`
	ConfigRequest  *configRequest  `json:"config_request,omitempty"` // kinds write/delete/revert/restore
	ConfigAction   *configAction   `json:"config_action,omitempty"`
	RoomsRequest   *roomsRequest   `json:"rooms_request,omitempty"` // kind rooms (plan_rooms.go)
	RawRequests    []rawRequest    `json:"raw_requests,omitempty"`  // kind raw (plan_raw.go); hashed exactly
	BatchMembers   []storedPlan    `json:"batch_members,omitempty"` // kind batch (plan_batch.go): the member plans, in order; hashed by id and hash
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
	plan.Lang, plan.Words = planLanguage(e, env, plan) // what every text of a plan that asks speaks; a typed plan's is sealed (channelSeal)
	shownChannel := ""
	if e.level == tierTyped {
		if e.s.transport.Credential == "" { // unreachable: a session always holds one
			return storedPlan{}, errors.New("cannot derive the one-time code: no Relay credential")
		}
		if plan.Channel == "" {
			plan.Channel = detectChannel(e.s)
		}
		if refusal := channelRefusal(plan.Channel, e.s.homeLanguage); refusal != nil {
			return storedPlan{}, refusal
		}
		if !humanChannel(plan.Channel) { // a window or phone plan's code never leaves this process
			plan.code = confirmCode(e.s.transport.Credential, idHex, plan.Channel, plan.ExpiresAt)
		}
		shownChannel = plan.Channel
	}
	where := map[string]string{channelWindow: "a confirmation window on their computer", channelPhone: "a notification on their phone"}[shownChannel]
	if shownChannel == channelWindow && phoneLinkedHere(e.s.paths) {
		where = "a confirmation window on their computer and a notification on their linked phone (the first answer counts)"
	}
	// A selected server tempts the agent to prefix apply too; Claude Code's
	// allow rule (claude_apply_rule.go) matches only the bare command.
	bare := ""
	if name, _ := requestedServerSelection(); name != "" {
		bare = " Run that apply exactly as written, as its own command: no HA_NOVA_SERVER= or --server (apply follows its plan's server)."
	}
	if where != "" {
		env.NextStep = fmt.Sprintf("Show `say`. When the user has asked for this change and your workflow has reached this step (a skill's order comes first), run ha-nova apply --plan %s without --confirm and wait for its result (give the command up to 2 minutes): %s asks them. Never ask for a code in chat.%s", plan.ID, where, bare)
	}
	if e.level == tierPreview { // the request is not the approval: an agent once saved in the same turn
		env.NextStep = fmt.Sprintf("Show the preview ending with `say` and end your turn; run ha-nova apply --plan %s only after the user's own reply approves it.%s", plan.ID, bare)
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

// callPlanBasis: the acted entities (basisEntities), the before value of
// every field the call changes on the frozen targets (the state only when
// the service changes it, plus basisAttributes), and the normalized config
// digests (spec §2.1). Listener surpluses already show in tier and reasons.
func callPlanBasis(e *tierEngine, service string, frozen []string, data map[string]any) map[string]any {
	_, name := splitID(service)
	changesState := serviceAfterState[name] != "" || name == "toggle" || strings.HasPrefix(name, "alarm_arm_")
	entities := basisEntities(e)
	before := map[string]any{}
	for _, id := range frozen {
		st := e.states[id]
		fields := map[string]any{}
		domain, _ := splitID(id)
		if changesState || basisAttributes[domain] != nil {
			fields["state"] = st.state()
		}
		for _, key := range basisAttributes[domain] {
			fields[key] = st.attr(key)
		}
		for key := range data {
			fields[key] = beforeValue(id, key, st)
		}
		before[id] = fields
	}
	return map[string]any{"entities": entities, "before": before, "configs": e.basis, "consumers": basisConsumers(e, frozen)}
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
