package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	EntityID string `json:"entity_id,omitempty"`
	Action   string `json:"action"`
	Via      string `json:"via"`
	Class    string `json:"class"`
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
	acted     []string
	nodes     int
	reads     int // consumer config reads
	events    map[string]int
	listeners *listenerTable
	full      []fullRegistryRow
	fullRead  bool
}

func newTierEngine(s *relaySession) *tierEngine {
	return &tierEngine{s: s, reg: map[string]registryEntry{}, states: map[string]*entityState{}, configs: map[string]map[string]any{},
		basis: map[string]string{}, seen: map[string]bool{}, visited: map[string]bool{}, complete: true}
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

// gap is an unresolved item: typed and coverage incomplete (I4).
func (e *tierEngine) gap(code, entityID, via, detail string) {
	e.complete = false
	e.raise(tierTyped, tierReason{Code: code, EntityID: entityID, Via: via, Detail: detail})
}

func (e *tierEngine) loadRegistry() error {
	var compact struct {
		Entities []struct{ EI, PL, DI string } `json:"entities"`
	}
	if err := e.s.wsDecode(map[string]any{"type": "config/entity_registry/list_for_display"}, &compact, "the compact entity registry"); err != nil {
		return err
	}
	for _, row := range compact.Entities {
		e.reg[row.EI] = registryEntry{pl: row.PL, di: row.DI}
		if row.PL == "template" && len(e.notes) == 0 {
			e.notes = append(e.notes, noteTemplates)
		}
	}
	return nil
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

// visit classifies one acted entity under one service and follows whatever
// that call forwards to. depth 0 is a direct (frozen) target.
func (e *tierEngine) visit(id, service string, data map[string]any, depth int, via string) {
	key := id + " " + service
	if e.visited[key] { // repeat or cycle: already classified or already flagged
		return
	}
	e.visited[key] = true
	if e.cut(depth, id, via) {
		return
	}
	st, err := e.state(id)
	if err != nil {
		e.gap(reasonScanFailed, id, via, err.Error())
		return
	}
	domain, _ := splitID(id)
	eff := effectiveService(service, domain)
	level, code := classifyAction(eff, data, st)
	e.raise(level, tierReason{Code: code, EntityID: id, Via: via})
	entry, registered := e.reg[id]
	switch pl := entry.pl; {
	case !registered && st != nil:
		// Platform unknown (YAML without unique_id): never ordinary; typed
		// when access-capable (spec §2.2 step 1).
		if accessCapable(id, st.str("device_class")) {
			level = tierTyped
			e.gap(reasonNoRegistryRow, id, via, "")
		} else {
			level, e.complete = max(level, tierPreview), false
			e.raise(tierPreview, tierReason{Code: reasonNoRegistryRow, EntityID: id, Via: via})
		}
	case pl == "template":
		level = tierTyped
		e.gap(reasonTemplateAction, id, via, "")
	case eff == "button.press" && (pl == "esphome" || pl == "mqtt"):
		level = tierTyped
		e.gap(reasonUserButton, id, via, pl)
	}
	if depth > 0 {
		e.members = append(e.members, tierMember{EntityID: id, Action: eff, Via: via, Class: level.String()})
	}
	switch {
	case isRun(domain, service):
		e.expandRun(id, domain, depth)
	case groupMembers(domain, st) != nil:
		for _, leaf := range groupMembers(domain, st) {
			e.visit(leaf, service, data, depth+1, id)
		}
	}
	if domain == "timer" {
		e.checkEvents("timer.", id)
	}
	e.scanConsumers(id, depth)
}

// groupMembers returns attributes.entity_id of a group-like entity, or nil.
func groupMembers(domain string, st *entityState) []string {
	if domain == "scene" || domain == "script" || domain == "automation" {
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
	if err == nil && st == nil {
		err = errors.New("entity not found")
	}
	if err != nil {
		e.gap(reasonScanFailed, id, via, err.Error())
		return nil, false
	}
	var notFound *verbFailure
	configID, err := resolveConfigID(e.s, id, domain, st.raw)
	var raw json.RawMessage
	if err == nil {
		raw, err = readStoredConfig(e.s, domain, configID)
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
		e.gap(reasonScanFailed, id, via, err.Error())
		return nil, false
	}
	return e.parseConfig(id, domain, configID, raw, via)
}

// parseConfig decodes a stored config, substitutes use_blueprint, and
// records the normalized digest in the plan basis.
func (e *tierEngine) parseConfig(id, domain, configID string, raw json.RawMessage, via string) (map[string]any, bool) {
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		e.gap(reasonScanFailed, id, via, "config is not a JSON object")
		return nil, false
	}
	if bp, isBlueprint := cfg["use_blueprint"].(map[string]any); isBlueprint {
		var sub struct {
			Config map[string]any `json:"substituted_config"`
		}
		err := e.s.wsDecode(map[string]any{"type": "blueprint/substitute", "domain": domain, "path": bp["path"], "input": bp["input"]}, &sub, "the blueprint substitution")
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

func (e *tierEngine) integrationScene(id, pl string) {
	rows, err := e.fullRegistry()
	if err != nil {
		e.gap(reasonScanFailed, id, "", err.Error())
		return
	}
	for _, row := range rows {
		dc := row.DeviceClass
		if dc == "" {
			dc = row.OriginalDeviceClass
		}
		if row.Platform == pl && accessCapable(row.EntityID, dc) {
			e.gap(reasonSceneAccess, id, "", pl+" provides "+row.EntityID)
			return
		}
	}
	e.complete = false
	e.raise(tierPreview, tierReason{Code: reasonSceneUnread, EntityID: id, Detail: pl})
}

func (e *tierEngine) sceneMembers(entities any, via string, depth int) {
	members, _ := entities.(map[string]any)
	for _, id := range sortedKeys(members) {
		service, data := sceneMemberService(id, members[id])
		e.visit(id, service, data, depth, via)
	}
}
