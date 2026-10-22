package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config plans (typed-verbs spec §2.1 basis for write/delete/revert, §6 PR5):
// write and revert are previews (typed when the config reaches a typed
// action), delete is always typed. Nothing here writes; apply executes.

const (
	planWriteUsage  = "ha-nova plan write --domain automation|script [--id <config_id>] [--create] --file <draft.json>\n       ha-nova plan write --domain dashboard --id <url_path> [--create --title <title> [--icon <mdi:icon>]] --file <config.json>"
	planDeleteUsage = "ha-nova plan delete <automation.x|script.x> | --domain dashboard --id <url_path>"
	planRevertUsage = "ha-nova plan revert [--target <config_id|url_path|rooms record>] [--domain automation|script|dashboard|device_area]"
)

// Config ids per domain: automation ids as the UI and YAML use them; script
// ids are the script's own key (its object id).
var configIDPatterns = map[string]*regexp.Regexp{
	"automation": regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`),
	"script":     regexp.MustCompile(`^[a-z0-9_]{1,64}$`),
}

func validConfigID(domain, id string) bool {
	pattern := configIDPatterns[domain]
	return pattern != nil && pattern.MatchString(id)
}

// configRequest is what apply recomputes a config plan from.
type configRequest struct {
	Op       string          `json:"op"` // write | delete | revert | restore
	Domain   string          `json:"domain"`
	ConfigID string          `json:"config_id,omitempty"`
	Create   bool            `json:"create,omitempty"` // minted id: must be unused
	Minted   bool            `json:"minted,omitempty"` // the id was minted or derived, never chosen by the user
	EntityID string          `json:"entity_id,omitempty"`
	Draft    json.RawMessage `json:"draft,omitempty"`
	Snapshot string          `json:"snapshot,omitempty"` // restore: the Relay snapshot file the draft came from
	Title    string          `json:"title,omitempty"`    // dashboard create: the new shell's title
	Icon     string          `json:"icon,omitempty"`     // dashboard create: the new shell's icon
	// Dashboard create: the shell's flags (Home Assistant's defaults: false, true).
	RequireAdmin bool `json:"require_admin,omitempty"`
	HideSidebar  bool `json:"hide_sidebar,omitempty"`
	// Base: dashboard update — the config_digest the draft was built from
	// (get --dashboard); a different live config is drift.
	Base string `json:"base,omitempty"`
}

// configAction is the full operation identity the hash covers (§2.1).
type configAction struct {
	Op       string          `json:"op"`
	Domain   string          `json:"domain"`
	ConfigID string          `json:"config_id"`
	Path     string          `json:"path"`
	EntityID string          `json:"entity_id,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
	// Dashboards: Shell is the create payload, or the list entry a delete
	// removes; Default addresses the unlisted default dashboard (no url_path).
	Shell   json.RawMessage `json:"shell,omitempty"`
	Default bool            `json:"default_dashboard,omitempty"`
}

// configContext carries what planning read and execution needs (never hashed
// beyond its digest): the live config, absent on create.
type configContext struct {
	current json.RawMessage
	alias   string
}

func (req configRequest) validate() error {
	switch {
	case req.Op != "write" && req.Op != "delete" && req.Op != "revert" && req.Op != "restore":
		return fmt.Errorf("unknown config operation %q", req.Op)
	case req.Domain == "dashboard":
		return req.validateDashboard()
	case req.Title != "" || req.Icon != "" || req.RequireAdmin || req.HideSidebar || req.Base != "":
		return errors.New("--title and --icon are for a new dashboard (--domain dashboard --create)")
	case req.Domain != "automation" && req.Domain != "script":
		return errors.New("config plans cover automation, script, and dashboard only")
	case req.Op == "delete" && (!entityIDPattern.MatchString(req.EntityID) || !strings.HasPrefix(req.EntityID, req.Domain+".")):
		return fmt.Errorf("%q is not an %s entity_id", req.EntityID, req.Domain)
	case (req.Op == "write" || req.Op == "restore" || req.ConfigID != "") && !validConfigID(req.Domain, req.ConfigID):
		return fmt.Errorf("%q is not a %s config id", req.ConfigID, req.Domain)
	case req.Op == "restore" && (!req.Create || restoreDomain(req.Snapshot) != req.Domain):
		return errors.New("a restore is a create from an automations/ or scripts/ snapshot of its own domain")
	case req.Op == "write" || req.Op == "restore":
		draft, err := configObjectFromBytes(req.Draft)
		if id, carriesID := draft["id"]; err == nil && req.Domain == "script" && carriesID {
			return errors.New("a script config carries no id; its key is --id (or derived from the alias)")
		} else if err == nil && req.Op == "restore" && req.Domain == "automation" && id != req.ConfigID {
			return errors.New("a restored automation keeps its own id")
		}
		return err
	}
	return nil
}

// exactConfig validates a config object and returns its bytes with
// whitespace removed but key order untouched: Home Assistant evaluates
// variables and fields top-down, so the bytes POSTed are the bytes given.
func exactConfig(raw []byte) (json.RawMessage, map[string]any, error) {
	obj, err := configObjectFromBytes(raw)
	if err != nil {
		return nil, nil, err
	}
	var compact bytes.Buffer
	err = json.Compact(&compact, raw)
	return compact.Bytes(), obj, err
}

var nonScriptKey = regexp.MustCompile(`[^a-z0-9]+`)

// scriptKey derives a script's key from its alias as the HA editor does.
func scriptKey(alias string) string {
	folded := diacriticStrip.Replace(strings.ToLower(stripCombiningMarks(alias)))
	return strings.Trim(nonScriptKey.ReplaceAllString(folded, "_"), "_")
}

func runPlanConfig(paths runtimePaths, op string, args []string) int {
	var opts verbOptions
	req := configRequest{Op: op}
	fs := flag.NewFlagSet("plan "+op, flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	usage := map[string]string{"write": planWriteUsage, "delete": planDeleteUsage, "revert": planRevertUsage, "restore": planRestoreUsage}[op]
	draftFile := ""
	if op == "write" || op == "revert" || op == "delete" {
		fs.StringVar(&req.Domain, "domain", "", "automation, script, or dashboard")
	}
	switch op {
	case "write":
		fs.StringVar(&req.ConfigID, "id", "", "config id (dashboard: url_path) to update or create with; minted when absent")
		fs.BoolVar(&req.Create, "create", false, "a new item: refuse if the config id already exists (never overwrites)")
		fs.StringVar(&draftFile, "file", "", "the full draft config as a JSON object")
		fs.StringVar(&req.Title, "title", "", "dashboard --create: the new dashboard's title")
		fs.StringVar(&req.Icon, "icon", "", "dashboard --create: the new dashboard's icon (mdi:name)")
		fs.BoolVar(&req.RequireAdmin, "require-admin", false, "dashboard --create: only administrators see it")
		fs.BoolVar(&req.HideSidebar, "hide-sidebar", false, "dashboard --create: not shown in the sidebar")
		fs.StringVar(&req.Base, "base", "", "dashboard update: the config_digest from get --dashboard that the draft starts from")
	case "delete":
		fs.StringVar(&req.ConfigID, "id", "", "--domain dashboard: the url_path of the dashboard to delete")
	case "revert":
		fs.StringVar(&req.ConfigID, "target", "", "config id of the update to revert (default: the newest)")
	case "restore":
		fs.StringVar(&req.Snapshot, "snapshot", "", "the snapshot file the delete result named")
		fs.StringVar(&req.ConfigID, "id", "", "the deleted item's config id (required for a script)")
	}
	positional, err := parseVerbArgs(fs, &opts, args, usage+"\n\nRead-only: stores a plan; \"ha-nova apply\" executes it.")
	if err == nil {
		err = req.parseInput(positional, draftFile)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		if req.Op == "revert" && req.ConfigID != "" { // a named rooms record reverts as a rooms plan (plan_rooms.go)
			if snap, err := selectServerUndoSnapshot(paths, s.server, req.ConfigID, req.Domain); err == nil && snap.Op == "rooms" {
				return planRoomsUndo(paths, s, env, snap)
			}
		}
		if req.Op == "restore" {
			if err := req.loadRestoreSnapshot(s); err != nil {
				return err
			}
		}
		_, plan, _, err := computeConfigPlan(paths, s, env, req)
		if err != nil {
			return err
		}
		return savePlan(paths, plan, s.transport.Credential)
	})
}

func (req *configRequest) parseInput(positional []string, draftFile string) error {
	switch req.Op {
	case "restore":
		return req.parseRestore(positional)
	case "delete":
		if req.Domain == "dashboard" && len(positional) == 0 {
			return req.validate()
		}
		if len(positional) != 1 || req.Domain != "" || req.ConfigID != "" {
			return errors.New("Usage: " + planDeleteUsage)
		}
		req.EntityID = positional[0]
		req.Domain, _ = splitID(req.EntityID)
	case "revert":
		if len(positional) != 0 || (req.Domain != "" && req.ConfigID == "") {
			return errors.New("Usage: " + planRevertUsage + " (--domain needs --target)")
		}
		return nil
	case "write":
		if len(positional) != 0 || draftFile == "" {
			return errors.New("Usage: " + planWriteUsage)
		}
		raw, err := os.ReadFile(draftFile)
		if err != nil {
			return err
		}
		exact, draft, err := exactConfig(raw)
		if err != nil {
			return fmt.Errorf("--file must hold one JSON object: %w", err)
		}
		req.Draft = exact
		if req.Domain == "dashboard" { // the url_path is always given (--id), never minted
			return req.validate()
		}
		if _, carriesID := draft["id"]; carriesID && req.Create && req.Domain == "automation" {
			return errors.New("a new automation's draft must not carry an id (a copied id would collide); remove it, or pass --id")
		}
		if id, _ := draft["id"].(string); id != "" && req.Domain == "automation" {
			if req.ConfigID != "" && req.ConfigID != id {
				return fmt.Errorf("the draft's id %q differs from --id %q", id, req.ConfigID)
			}
			req.ConfigID = id
		}
		switch {
		case req.ConfigID != "":
		case req.Domain == "script": // a create: the key comes from the alias, like the script editor
			alias, _ := draft["alias"].(string)
			if req.ConfigID, req.Create, req.Minted = scriptKey(alias), true, true; req.ConfigID == "" && strings.TrimSpace(alias) != "" {
				return errors.New("the alias has no a-z/0-9 characters to derive a key from; pass --id")
			} else if req.ConfigID == "" {
				return errors.New("a new script needs --id or an alias to derive its key from")
			}
		default: // an automation create: the Home Assistant UI mints a millisecond id the same way
			req.ConfigID, req.Create, req.Minted = strconv.FormatInt(verbNow().UnixMilli(), 10), true, true
		}
	}
	return req.validate()
}

// computeConfigPlan reads the live config, resolves ids, classifies what the
// written config would actuate, and builds the plan (not stored).
func computeConfigPlan(paths runtimePaths, s *relaySession, env *verbEnvelope, req configRequest) (*tierEngine, storedPlan, configContext, error) {
	var ctx configContext
	fail := func(err error) (*tierEngine, storedPlan, configContext, error) { return nil, storedPlan{}, ctx, err }
	if req.Domain == "dashboard" {
		return computeDashboardPlan(paths, s, env, req)
	}
	e := newTierEngine(s)
	e.loadRegistry()
	if err := s.unreachableError(); err != nil {
		return fail(err)
	}
	var snap undoSnapshot
	switch req.Op {
	case "delete":
		st, err := e.state(req.EntityID)
		if err != nil {
			return fail(err)
		}
		if st != nil {
			req.ConfigID, err = resolveConfigID(s, req.EntityID, req.Domain, st.raw)
		} else { // a registry-disabled item has no state: its unique_id is the config id
			req.ConfigID, err = e.registryConfigID(req.EntityID)
		}
		var missing *verbFailure
		if errors.As(err, &missing) && missing.code == "CONFIG_NOT_FOUND" {
			if _, yaml, loadErr := loadedConfig(s, req.EntityID, req.Domain); loadErr != nil {
				return fail(loadErr)
			} else if yaml { // an id-less YAML item: loaded, not UI-managed
				return fail(refused("CONFIG_NOT_UI_MANAGED", req.EntityID+" is defined in YAML or a package, not in the UI config; delete it there with ha-nova:yaml-config"))
			}
		}
		if err != nil {
			return fail(err)
		}
	case "revert":
		var err error
		if snap, err = selectServerUndoSnapshot(paths, s.server, req.ConfigID, req.Domain); err != nil {
			return fail(refused("NO_UNDO_SNAPSHOT", err.Error()))
		}
		if snap.Op == "rooms" { // only by name: "undo the last update" never undoes setup's rooms
			return fail(refusedWith("REVERT_UNSUPPORTED", fmt.Sprintf("the newest checkpoint is a room change from setup (%s), not an automation, script, or dashboard update", snap.Name),
				fmt.Sprintf("Ask the user which change they mean. Setup's room change: ha-nova plan revert --target %s, only if they mean that. Otherwise pass --target <config_id> of the automation, script, or dashboard.", snap.TargetID)))
		}
		if snap.Op != "update" || (snap.Domain != "automation" && snap.Domain != "script" && snap.Domain != "dashboard") {
			// Helper checkpoints share the stack; their revert is ha-nova:helper's
			// manual path (update-revert.md), never this verb.
			return fail(refused("REVERT_UNSUPPORTED", fmt.Sprintf("the newest checkpoint is a %s %s; revert it through ha-nova:helper, or pass --target for an automation/script/dashboard", snap.Domain, snap.Op)))
		}
		if snap.Domain == "dashboard" {
			req.ConfigID, req.Domain = snap.TargetID, snap.Domain
			return computeDashboardPlan(paths, s, env, req)
		}
		req.ConfigID, req.Domain = snap.TargetID, snap.Domain
		if req.Draft, _, err = exactConfig(snap.BeforeConfig); err != nil { // restored byte-for-byte, order intact
			return fail(err)
		}
	}
	if !validConfigID(req.Domain, req.ConfigID) {
		return fail(refused("CONFIG_ID_UNSUPPORTED", fmt.Sprintf("%q is not a %s config id the typed verbs can address", req.ConfigID, req.Domain)))
	}
	current, err := readStoredConfig(s, req.Domain, req.ConfigID)
	var notFound *verbFailure
	switch {
	case errors.As(err, &notFound):
		current = nil
	case err != nil:
		return fail(err)
	}
	ctx.current = current
	if current == nil && req.Op != "revert" {
		// A 404 can hide a YAML or package item that owns this id: never
		// create over it, never call it missing.
		rows, err := e.fullRegistry()
		if err != nil {
			return fail(err)
		}
		for _, row := range rows {
			if row.Platform == req.Domain && row.UniqueID == req.ConfigID {
				return fail(refused("CONFIG_NOT_UI_MANAGED", row.EntityID+" is defined in YAML or a package, not in the UI config; edit it with ha-nova:yaml-config"))
			}
		}
	}
	switch {
	case req.Op == "delete" && current == nil:
		return fail(&verbFailure{exit: verbExitNotFound, code: "CONFIG_NOT_FOUND", message: req.EntityID + " has no stored config to delete"})
	case req.Op == "restore" && current != nil:
		return fail(restoreIDInUse(req.ConfigID))
	case req.Op == "write" && req.Create && current != nil:
		return fail(refusedWith("CONFIG_ID_IN_USE", "config id "+req.ConfigID+" already exists; a create never overwrites", configIDInUseNext(req.Domain)))
	case req.Op == "revert":
		if live := current; live == nil {
			return fail(revertDrift(req.ConfigID + " no longer exists"))
		} else if matches, err := snapshotMatchesLive(snap, live); err != nil || !matches {
			return fail(revertDrift(req.ConfigID + " changed since its last verified update"))
		}
	}
	action := configAction{Op: req.Op, Domain: req.Domain, ConfigID: req.ConfigID, Path: "/api/config/" + req.Domain + "/config/" + url.PathEscape(req.ConfigID), EntityID: req.EntityID}
	if req.Op == "delete" {
		e.raise(tierTyped, tierReason{Code: reasonConfigDelete, EntityID: req.EntityID})
		e.referencedBy(req.EntityID)
	} else {
		action.Body = req.Draft
		e.raise(tierPreview, tierReason{Code: map[string]string{"write": reasonConfigWrite, "revert": reasonConfigRevert, "restore": reasonConfigRestore}[req.Op], Via: req.Domain + "." + req.ConfigID})
		e.classifyDraft(req.Domain, req.ConfigID, req.Draft)
		if env.Verb == "plan" && req.Op == "write" && req.Domain == "automation" { // apply re-plans: the plan said it already
			env.Notices = append(env.Notices, draftTriggerNotices(req.Draft, current)...)
		}
		// A create or restore has no before side: its rows would be the whole
		// config as raw JSON, which agents pasted into a plain-language
		// preview (F2). The preview summarizes it instead; config.body
		// keeps the details.
		if !req.Create {
			before := current
			if before == nil {
				before = json.RawMessage(`{}`)
			}
			withPreviewNames(loadPreviewNames(s, req.Draft), func() { env.Diff, _ = configChanges(before, req.Draft) })
		}
		after, _ := configObjectFromBytes(req.Draft)
		ctx.alias, _ = after["alias"].(string)
	}
	plan := storedPlan{Kind: req.Op, ConfigRequest: &req, ConfigAction: &action, Basis: configPlanBasis(e, current, req.Draft)}
	plan, err = sealPlan(e, env, plan, action)
	env.Config = &action
	if env.Status == "needs_confirmation" {
		// The form names what is there now; the write card names the draft.
		env.Say, plan.Prompt = confirmationSay(e, env, plan, configAlias(req.Draft, current)), confirmationPrompt(e, env, plan, configAlias(current, req.Draft))
	}
	return e, plan, ctx, err
}

// registryConfigID resolves an entity without a state (registry-disabled)
// through its registry row; no row is NOT_FOUND with candidates.
func (e *tierEngine) registryConfigID(entityID string) (string, error) {
	rows, err := e.fullRegistry()
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.EntityID == entityID && row.UniqueID != "" {
			return row.UniqueID, nil
		}
	}
	_, _, err = readEntityState(e.s, entityID)
	return "", err
}

// configAlias names a config item for the confirmation text: the draft's
// alias, else the live config's.
func configAlias(draft, current json.RawMessage) string {
	for _, raw := range []json.RawMessage{draft, current} {
		if cfg, err := configObjectFromBytes(raw); err == nil {
			if alias, _ := cfg["alias"].(string); alias != "" {
				return alias
			}
		}
	}
	return ""
}

func revertDrift(message string) *verbFailure {
	return &verbFailure{exit: verbExitDrift, status: "drift", code: "REVERT_DRIFT",
		message: message + "; reverting would discard that change, so nothing was planned",
		next:    "Review the live config and write the change you want instead."}
}

// classifyDraft walks what the written config would run, like a stored
// member (rules of indirect-actuation.md), so a write that makes an
// automation unlock a door is typed.
func (e *tierEngine) classifyDraft(domain, configID string, draft json.RawMessage) {
	cfg, ok := e.parseConfig("draft "+domain+"."+configID, domain, configID+"#draft", draft, "draft")
	if !ok {
		return
	}
	e.draft = true
	if watchesConfirmation(cfg) {
		e.raise(tierTyped, tierReason{Code: reasonConfirmWatch, EntityID: domain + "." + configID, Via: "draft"})
	}
	if domain == "script" {
		e.walk(cfg["sequence"], "draft/sequence", 1)
		return
	}
	e.walk(automationActions(cfg), "draft/actions", 1)
}

// referencedBy lists what references the item a delete removes.
func (e *tierEngine) referencedBy(entityID string) {
	related, err := e.related("entity", entityID)
	if err != nil || related == nil { // a null answer is no scan, never "none"
		e.readFailed(entityID, "", "related")
		return
	}
	for _, key := range []string{"automation", "script", "scene"} {
		for _, id := range related[key] {
			if id != entityID {
				e.consumers = append(e.consumers, tierConsumer{Automation: id, Match: "reference", Class: "breaks"})
			}
		}
	}
}

// configPlanBasis: the normalized live config digest ("absent" on create),
// the draft digest, every member config digest, and the acted entities
// (basisEntities). Key order and HA bookkeeping keys never
// count as drift (normalizeConfig).
func configPlanBasis(e *tierEngine, current, draft json.RawMessage) map[string]any {
	digest := func(raw json.RawMessage) string {
		if raw == nil {
			return "absent"
		}
		obj, err := configObjectFromBytes(raw)
		if err != nil {
			return "unreadable"
		}
		normalized, _ := json.Marshal(map[string]any{"content": normalizeConfig(obj), "order": orderedKeyLists(raw)})
		sum := sha256.Sum256(normalized)
		return hex.EncodeToString(sum[:])
	}
	return map[string]any{"current": digest(current), "draft": digest(draft), "configs": e.basis, "entities": basisEntities(e)}
}
