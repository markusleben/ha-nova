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
	planWriteUsage  = "ha-nova plan write --domain automation|script [--id <config_id>] --file <draft.json>"
	planDeleteUsage = "ha-nova plan delete <automation.x|script.x>"
	planRevertUsage = "ha-nova plan revert [--target <config_id>] [--domain automation|script]"
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
	Op       string          `json:"op"` // write | delete | revert
	Domain   string          `json:"domain"`
	ConfigID string          `json:"config_id,omitempty"`
	Create   bool            `json:"create,omitempty"` // minted id: must be unused
	EntityID string          `json:"entity_id,omitempty"`
	Draft    json.RawMessage `json:"draft,omitempty"`
}

// configAction is the full operation identity the hash covers (§2.1).
type configAction struct {
	Op       string          `json:"op"`
	Domain   string          `json:"domain"`
	ConfigID string          `json:"config_id"`
	Path     string          `json:"path"`
	EntityID string          `json:"entity_id,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
}

// configContext carries what planning read and execution needs (never hashed
// beyond its digest): the live config, absent on create.
type configContext struct {
	current json.RawMessage
	alias   string
}

func (req configRequest) validate() error {
	switch {
	case req.Op != "write" && req.Op != "delete" && req.Op != "revert":
		return fmt.Errorf("unknown config operation %q", req.Op)
	case req.Domain != "automation" && req.Domain != "script":
		return errors.New("config plans cover automation and script only")
	case req.Op == "delete" && (!entityIDPattern.MatchString(req.EntityID) || !strings.HasPrefix(req.EntityID, req.Domain+".")):
		return fmt.Errorf("%q is not an %s entity_id", req.EntityID, req.Domain)
	case (req.Op == "write" || req.ConfigID != "") && !validConfigID(req.Domain, req.ConfigID):
		return fmt.Errorf("%q is not a %s config id", req.ConfigID, req.Domain)
	case req.Op == "write":
		draft, err := configObjectFromBytes(req.Draft)
		if _, carriesID := draft["id"]; err == nil && req.Domain == "script" && carriesID {
			return errors.New("a script config carries no id; its key is --id (or derived from the alias)")
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
	usage := map[string]string{"write": planWriteUsage, "delete": planDeleteUsage, "revert": planRevertUsage}[op]
	draftFile := ""
	if op != "delete" {
		fs.StringVar(&req.Domain, "domain", "", "automation or script")
	}
	switch op {
	case "write":
		fs.StringVar(&req.ConfigID, "id", "", "config id to update (or create with); minted when absent")
		fs.StringVar(&draftFile, "file", "", "the full draft config as a JSON object")
	case "revert":
		fs.StringVar(&req.ConfigID, "target", "", "config id of the update to revert (default: the newest)")
	}
	positional, err := parseVerbArgs(fs, &opts, args, usage+"\n\nRead-only: stores a plan; \"ha-nova apply\" executes it.")
	if err == nil {
		err = req.parseInput(positional, draftFile)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		_, plan, _, err := computeConfigPlan(paths, s, env, req)
		if err != nil {
			return err
		}
		return savePlan(paths, plan)
	})
}

func (req *configRequest) parseInput(positional []string, draftFile string) error {
	switch req.Op {
	case "delete":
		if len(positional) != 1 {
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
			if req.ConfigID, req.Create = scriptKey(alias), true; req.ConfigID == "" {
				return errors.New("a new script needs --id or an alias to derive its key from")
			}
		default: // an automation create: the Home Assistant UI mints a millisecond id the same way
			req.ConfigID, req.Create = strconv.FormatInt(verbNow().UnixMilli(), 10), true
		}
	}
	return req.validate()
}

// computeConfigPlan reads the live config, resolves ids, classifies what the
// written config would actuate, and builds the plan (not stored).
func computeConfigPlan(paths runtimePaths, s *relaySession, env *verbEnvelope, req configRequest) (*tierEngine, storedPlan, configContext, error) {
	var ctx configContext
	fail := func(err error) (*tierEngine, storedPlan, configContext, error) { return nil, storedPlan{}, ctx, err }
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
		if err != nil {
			return fail(err)
		}
	case "revert":
		var err error
		if snap, err = selectServerUndoSnapshot(paths, s.server, req.ConfigID, req.Domain); err != nil {
			return fail(refused("NO_UNDO_SNAPSHOT", err.Error()))
		}
		if snap.Op != "update" || (snap.Domain != "automation" && snap.Domain != "script") {
			return fail(refused("REVERT_UNSUPPORTED", fmt.Sprintf("the newest checkpoint is a %s %s; revert it through ha-nova:write", snap.Domain, snap.Op)))
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
	case req.Op == "write" && req.Create && current != nil:
		return fail(refused("CONFIG_ID_IN_USE", "the minted config id "+req.ConfigID+" already exists; plan again"))
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
		e.raise(tierPreview, tierReason{Code: map[string]string{"write": reasonConfigWrite, "revert": reasonConfigRevert}[req.Op], Via: req.Domain + "." + req.ConfigID})
		e.classifyDraft(req.Domain, req.ConfigID, req.Draft)
		before := current
		if before == nil {
			before = json.RawMessage(`{}`)
		}
		env.Diff, _ = configChanges(before, req.Draft)
		after, _ := configObjectFromBytes(req.Draft)
		ctx.alias, _ = after["alias"].(string)
	}
	plan := storedPlan{Kind: req.Op, ConfigRequest: &req, ConfigAction: &action, Basis: configPlanBasis(e, current, req.Draft)}
	plan, err = sealPlan(e, env, plan, action)
	env.Config = &action
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
	if domain == "script" {
		e.walk(cfg["sequence"], "draft/sequence", 1)
		return
	}
	e.walk(automationActions(cfg), "draft/actions", 1)
}

// referencedBy lists what references the item a delete removes.
func (e *tierEngine) referencedBy(entityID string) {
	related, err := e.related("entity", entityID)
	if err != nil {
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
// the draft digest, every member config digest, and the acted entities'
// pl/device_class/supported_features. Key order and HA bookkeeping keys never
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
	entities := map[string]any{}
	for _, id := range e.acted {
		st := e.states[id]
		entities[id] = map[string]any{"pl": e.reg[id].pl, "device_class": st.attr("device_class"), "supported_features": st.attr("supported_features")}
	}
	return map[string]any{"current": digest(current), "draft": digest(draft), "configs": e.basis, "entities": entities}
}
