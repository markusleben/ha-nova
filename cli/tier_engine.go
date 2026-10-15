package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// Deterministic tiering (typed-verbs spec §2.2, indirect-actuation.md).
// Every read failure, unread user-authored item, or limit cut inside the
// traversal is typed (I4); the engine never defaults down.

type tierReason struct {
	Code     string `json:"code"`
	EntityID string `json:"entity_id,omitempty"`
	Via      string `json:"via,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Skill    string `json:"skill,omitempty"`
}

type tierMember struct {
	EntityID string         `json:"entity_id,omitempty"`
	Action   string         `json:"action"`
	Via      string         `json:"via"`
	Class    string         `json:"class"`
	data     map[string]any // what the member sets (verification probes)
}

type tierConsumer struct {
	Automation string `json:"automation"`
	Match      string `json:"match"`
	Class      string `json:"class"`
}

type tierCoverage struct {
	Complete bool     `json:"complete"`
	Notes    []string `json:"notes,omitempty"`
}

type entityState struct {
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
	raw        json.RawMessage
}

func (st *entityState) attr(key string) any {
	if st == nil {
		return nil
	}
	return st.Attributes[key]
}

func (st *entityState) str(key string) string  { s, _ := st.attr(key).(string); return s }
func (st *entityState) num(key string) float64 { n, _ := st.attr(key).(float64); return n }

type registryEntry struct{ pl, di string }

type tierEngine struct {
	s         *relaySession
	reg       map[string]registryEntry
	states    map[string]*entityState // nil value: entity absent (HTTP 404)
	configs   map[string]map[string]any
	basis     map[string]string // config key → sha256 of the normalized config
	level     tierLevel
	scopes    []*tierLevel
	reasons   []tierReason
	seen      map[string]bool
	members   []tierMember
	consumers []tierConsumer
	notes     []string
	complete  bool
	visited   map[string]bool
	done      map[string]tierLevel // finished visit/consumer → the class it reached
	acted     []string
	nodes     int
	reads     int // consumer config reads
	events    map[string]int
	listeners *listenerTable
	full      []fullRegistryRow
	regFailed bool     // the compact registry could not be read
	groups    []string // expanded direct group targets: consumer-scanned, never frozen
	fullRead  bool
}

func newTierEngine(s *relaySession) *tierEngine {
	return &tierEngine{s: s, reg: map[string]registryEntry{}, states: map[string]*entityState{}, configs: map[string]map[string]any{},
		basis: map[string]string{}, seen: map[string]bool{}, visited: map[string]bool{}, done: map[string]tierLevel{}, complete: true}
}

// raise records a reason and lifts the run (and every open consumer scope).
func (e *tierEngine) raise(level tierLevel, r tierReason) {
	if level > e.level {
		e.level = level
	}
	for _, scope := range e.scopes {
		if level > *scope {
			*scope = level
		}
	}
	key := r.Code + "|" + r.EntityID + "|" + r.Via + "|" + r.Detail
	if r.Code != "" && !e.seen[key] {
		e.seen[key] = true
		e.reasons = append(e.reasons, r)
	}
}

// lift carries an already-classified subtree's class into the run and every
// open scope, without repeating its reasons.
func (e *tierEngine) lift(level tierLevel) { e.raise(level, tierReason{}) }

// readFailed records a failed read as typed: LIMIT_CUT once the 30 s budget
// is spent, SCAN_FAILED otherwise. Details are stable words, never error
// text, so the plan hash stays deterministic.
func (e *tierEngine) readFailed(entityID, via, what string) {
	if e.s.ctx.Err() != nil {
		e.gap(reasonLimitCut, "", "", "read budget")
		return
	}
	e.gap(reasonScanFailed, entityID, via, what)
}

// prefetchStates warms the session memo with independent state reads; the
// session caps them at 8 in flight, and the sequential pass then reads the
// memo in a fixed order (results and errors alike), so output stays stable.
func (e *tierEngine) prefetchStates(ids []string) {
	var reads []func()
	for _, id := range ids {
		if _, known := e.states[id]; !known && entityIDPattern.MatchString(id) {
			reads = append(reads, func() { _, _, _ = e.s.coreGET("/api/states/" + id) })
		}
	}
	prefetch(reads)
}

func prefetch(reads []func()) {
	var wg sync.WaitGroup
	for _, read := range reads {
		wg.Add(1)
		go func() { defer wg.Done(); read() }()
	}
	wg.Wait()
}

func (e *tierEngine) note(code string) {
	if !slices.Contains(e.notes, code) {
		e.notes = append(e.notes, code)
	}
}

// gap is an unresolved item: typed and coverage incomplete (I4).
func (e *tierEngine) gap(code, entityID, via, detail string) {
	e.complete = false
	e.raise(tierTyped, tierReason{Code: code, EntityID: entityID, Via: via, Detail: detail})
}

// loadRegistry reads the compact registry; on failure every entity reads
// as unregistered (never ordinary) and the run is typed.
func (e *tierEngine) loadRegistry() {
	var compact struct {
		Entities []struct{ EI, PL, DI string } `json:"entities"`
	}
	if err := e.s.wsDecode(map[string]any{"type": "config/entity_registry/list_for_display"}, &compact, "the compact entity registry"); err != nil {
		e.readFailed("", "", "registry")
		e.regFailed = true
		return
	}
	for _, row := range compact.Entities {
		e.reg[row.EI] = registryEntry{pl: row.PL, di: row.DI}
		if row.PL == "template" {
			e.note(noteTemplates)
		}
	}
}

func (e *tierEngine) state(id string) (*entityState, error) {
	if st, ok := e.states[id]; ok {
		return st, nil
	}
	if !entityIDPattern.MatchString(id) {
		return nil, fmt.Errorf("%q is not a literal entity_id", id)
	}
	status, body, err := e.s.coreGET("/api/states/" + id)
	if err != nil {
		return nil, err
	}
	var st *entityState
	switch {
	case status == http.StatusOK:
		st = &entityState{raw: body}
		if err := json.Unmarshal(body, st); err != nil {
			return nil, fmt.Errorf("cannot parse the state of %s: %w", id, err)
		}
	case status != http.StatusNotFound:
		return nil, fmt.Errorf("Home Assistant answered HTTP %d for %s", status, id)
	}
	e.states[id] = st
	return st, nil
}

// cut enforces the node and depth budgets; a cut is never a clean result.
func (e *tierEngine) cut(depth int, id, via string) bool {
	e.nodes++
	switch {
	case depth > tierMaxDepth:
		e.gap(reasonLimitCut, id, via, fmt.Sprintf("deeper than %d", tierMaxDepth))
	case e.nodes > tierMaxNodes:
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d nodes", tierMaxNodes))
	default:
		return false
	}
	return true
}

// visit classifies one acted entity under one service and data, then
// follows whatever that call forwards to. depth 0 is a direct (frozen) target.
func (e *tierEngine) visit(id, service string, data map[string]any, depth int, via string) {
	payload, _ := json.Marshal(data)
	key := id + " " + service + " " + string(payload)
	if e.visited[key] { // repeat or cycle: classified (or flagged) already
		if level, finished := e.done[key]; finished {
			e.lift(level)
		}
		return
	}
	e.visited[key] = true
	if e.cut(depth, id, via) {
		return
	}
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	e.classify(id, service, data, depth, via)
	e.scopes = e.scopes[:len(e.scopes)-1]
	e.done[key] = class
}

func (e *tierEngine) classify(id, service string, data map[string]any, depth int, via string) {
	st, err := e.state(id)
	if err != nil {
		e.readFailed(id, via, "state")
		return
	}
	domain, _ := splitID(id)
	eff := effectiveService(service, domain)
	level, code := classifyOnEntity(id, eff, data, st)
	e.raise(level, tierReason{Code: code, EntityID: id, Via: via})
	entry, registered := e.reg[id]
	switch pl := entry.pl; {
	case !registered && st != nil:
		// Platform unknown (YAML without unique_id): never ordinary; typed
		// for an actuator (spec §2.2 step 1) — a YAML group too: without a
		// platform an entity_id list proves nothing (a template can expose
		// one); its members are still expanded and classified below.
		switch {
		case unregisteredActuatorDomains[domain]: // a YAML template/command_line/rest/mqtt entity: its action is whatever its author wired, unread
			level = tierTyped
			e.gap(reasonUnregisteredActuator, id, via, unregisteredActuatorDetail)
		default:
			level, e.complete = max(level, tierPreview), false
			e.raise(tierPreview, tierReason{Code: reasonNoRegistryRow, EntityID: id, Via: via})
		}
	case pl == "template":
		level = tierTyped
		e.gap(reasonTemplateAction, id, via, "")
	case userCommandPlatforms[pl]: // its command or request lives in configuration.yaml
		level = tierTyped
		e.gap(reasonUnreadCommand, id, via, pl)
	case eff == "button.press" && (pl == "esphome" || pl == "mqtt"):
		level = tierTyped
		e.gap(reasonUserButton, id, via, pl)
	}
	if serviceDomain, _ := splitID(eff); !coreDomains[serviceDomain] {
		level = max(level, e.deviceAccess(id, via, eff))
	}
	if domain == "switch" && entry.pl != "switch_as_x" {
		level = max(level, e.switchAsX(id, via))
	}
	if depth > 0 {
		e.members = append(e.members, tierMember{EntityID: id, Action: eff, Via: via, Class: level.String(), data: data})
	}
	switch leaves := groupMembers(domain, st); {
	case isRun(domain, service):
		e.expandRun(id, domain, depth)
	case leaves != nil:
		e.prefetchStates(leaves)
		for _, leaf := range leaves {
			e.visit(leaf, service, data, depth+1, id)
		}
	}
	if domain == "timer" {
		e.checkEvents("timer.", id)
	}
	e.scanConsumers(id, depth)
}

// notGroupDomains carry an attributes.entity_id list that is not group
// membership (a scene's targets).
var notGroupDomains = map[string]bool{"scene": true, "script": true, "automation": true}

// groupMembers returns attributes.entity_id of a group-like entity, or nil.
func groupMembers(domain string, st *entityState) []string {
	if notGroupDomains[domain] {
		return nil
	}
	list, isList := st.attr("entity_id").([]any)
	if !isList {
		return nil
	}
	members := []string{}
	for _, m := range list {
		if id, isString := m.(string); isString {
			members = append(members, id)
		}
	}
	return members
}

func (e *tierEngine) expandRun(id, domain string, depth int) {
	if depth == 0 {
		e.raise(tierPreview, tierReason{Code: reasonStoredMembers, EntityID: id})
	}
	switch domain { // a run also fires its own event; its listeners run too
	case "script":
		e.checkEvents("script_started", id)
	case "automation":
		e.checkEvents("automation_triggered", id)
	}
	cfg, ok := e.runConfig(id, domain, "")
	if !ok {
		return
	}
	switch domain {
	case "scene":
		e.sceneMembers(cfg["entities"], id, depth+1)
	case "script":
		e.walk(cfg["sequence"], id+"/sequence", depth+1)
	default:
		e.walk(automationActions(cfg), id+"/actions", depth+1)
	}
}

func automationActions(cfg map[string]any) any {
	if actions, ok := cfg["actions"]; ok {
		return actions
	}
	return cfg["action"]
}

// runConfig reads a stored config by its real config id (never the slug),
// expanding use_blueprint. Every miss is a named gap, except an integration
// scene, whose platform bounds what it can reach (indirect-actuation.md
// → When you cannot see the members).
func (e *tierEngine) runConfig(id, domain, via string) (map[string]any, bool) {
	if cfg, ok := e.configs[id]; ok {
		return cfg, cfg != nil
	}
	e.configs[id] = nil
	st, err := e.state(id)
	if err != nil || st == nil {
		e.readFailed(id, via, "state")
		return nil, false
	}
	var notFound *verbFailure
	configID, err := resolveConfigID(e.s, id, domain, st.raw)
	var raw json.RawMessage
	if err == nil {
		raw, err = readStoredConfig(e.s, domain, configID)
	}
	if errors.As(err, &notFound) { // YAML/package item: read what Home Assistant loaded
		loaded, ok, loadErr := loadedConfig(e.s, id, domain)
		if loadErr != nil {
			e.readFailed(id, via, "config")
			return nil, false
		}
		if ok {
			return e.parseConfig(id, domain, loadedConfigKey(id), loaded, via)
		}
	}
	switch pl := e.reg[id].pl; {
	case err == nil:
	case domain == "scene" && pl != "" && pl != "homeassistant" && errors.As(err, &notFound):
		e.integrationScene(id, pl)
		return nil, false
	case errors.As(err, &notFound) && configID == "":
		e.gap(reasonYAMLNoID, id, via, "")
		return nil, false
	case errors.As(err, &notFound):
		e.gap(reasonUnreadConfig, id, via, configID)
		return nil, false
	default:
		e.readFailed(id, via, "config")
		return nil, false
	}
	return e.parseConfig(id, domain, configID, raw, via)
}

// loadedConfig reads the config Home Assistant has loaded for an automation
// or script by entity_id (WS automation/config, script/config) — the only
// view of a YAML or package item that has no UI config. Safe to call
// concurrently (session reads only).
func loadedConfig(s *relaySession, entityID, domain string) (json.RawMessage, bool, error) {
	if domain != "automation" && domain != "script" {
		return nil, false, nil
	}
	var loaded struct {
		Config json.RawMessage `json:"config"`
	}
	err := s.wsDecode(map[string]any{"type": domain + "/config", "entity_id": entityID}, &loaded, "the loaded config of "+entityID)
	switch trimmed := strings.TrimSpace(string(loaded.Config)); {
	case isUpstreamNotFound(err):
		return nil, false, nil
	case err != nil: // permission, transport, decode: a failed read, never "missing"
		return nil, false, err
	case trimmed == "" || trimmed == "null":
		return nil, false, nil
	}
	return loaded.Config, true, nil
}

// loadedConfigKey names a loaded (YAML) config in the plan basis; it never
// collides with a stored config id.
func loadedConfigKey(entityID string) string { return "loaded:" + entityID }

// parseConfig decodes a stored config, substitutes use_blueprint, and
// records the normalized digest in the plan basis.
func (e *tierEngine) parseConfig(id, domain, configID string, raw json.RawMessage, via string) (map[string]any, bool) {
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		e.gap(reasonScanFailed, id, via, "config is not a JSON object")
		return nil, false
	}
	if bp, isBlueprint := cfg["use_blueprint"].(map[string]any); isBlueprint {
		input := bp["input"]
		if input == nil {
			input = map[string]any{}
		}
		var sub struct {
			Config map[string]any `json:"substituted_config"`
		}
		err := e.s.wsDecode(map[string]any{"type": "blueprint/substitute", "domain": domain, "path": bp["path"], "input": input}, &sub, "the blueprint substitution")
		if err != nil || sub.Config == nil {
			e.gap(reasonBlueprint, id, via, fmt.Sprint(bp["path"]))
			return nil, false
		}
		cfg = sub.Config
	}
	normalized, _ := json.Marshal(normalizeConfig(cfg))
	sum := sha256.Sum256(normalized)
	e.basis[domain+":"+configID] = hex.EncodeToString(sum[:])
	e.configs[id] = cfg
	return cfg, true
}
