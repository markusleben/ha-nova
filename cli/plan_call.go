package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
)

const planCallUsage = `ha-nova plan call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>] [--response] [--skill <owner>]`

const planCallHelp = planCallUsage + `

Read-only: resolves the targets, computes the confirmation tier, and stores
the plan for 15 minutes. plan call never executes a plan; "ha-nova apply"
does, and "ha-nova call" plans and runs ordinary calls in one step.
Service data may not carry entity_id, area_id, device_id, or target (pass
targets as arguments); script fields named "target" are not supported in v1.
Exit: 0 ordinary (plan stored), 3 needs confirmation, 4 refused, 5 ambiguous or not found.`

type planCallRequest struct {
	Service   string         `json:"service"`
	EntityIDs []string       `json:"entity_ids,omitempty"`
	Match     string         `json:"match,omitempty"`
	Area      string         `json:"area,omitempty"`
	Device    string         `json:"device,omitempty"`
	Data      map[string]any `json:"data"`
	// Skill: the owning skill an owned service is called from (--skill);
	// Response: the call returns Home Assistant's service_response.
	Skill    string `json:"skill,omitempty"`
	Response bool   `json:"response,omitempty"`
}

func runPlanCommand(paths runtimePaths, args []string) int {
	switch {
	case len(args) > 0 && args[0] == "call":
		return runPlanCall(paths, args[1:])
	case len(args) > 0 && (args[0] == "write" || args[0] == "delete" || args[0] == "revert" || args[0] == "restore"):
		return runPlanConfig(paths, args[0], args[1:])
	case len(args) > 0 && args[0] == "rooms":
		return runPlanRooms(paths, args[1:])
	case len(args) > 0 && args[0] == "template":
		return runPlanTemplate(paths, args[1:])
	case len(args) > 0 && args[0] == "raw":
		return runPlanRaw(paths, args[1:])
	case len(args) > 0 && args[0] == "batch":
		return runPlanBatch(paths, args[1:])
	case len(args) > 0 && (args[0] == "--help" || args[0] == "-h"):
		fmt.Println("Usage: " + planCallHelp)
		return verbExitOK
	}
	return verbUsageError("plan", errors.New("Usage: "+planCallUsage+" | "+planWriteUsage+" | "+planDeleteUsage+" | "+planRevertUsage+" | "+planRestoreUsage+" | "+planRoomsUsage+" | "+planTemplateUsage+" | "+planRawUsage+" | "+planBatchUsage))
}

func runPlanCall(paths runtimePaths, args []string) int {
	return runCallVerb(paths, "plan", "plan call", planCallHelp, args, func(s *relaySession, env *verbEnvelope, req planCallRequest) error {
		return planCall(paths, s, env, req)
	})
}

// runCallVerb parses the shared plan/call flags, answers static refusals
// without any Relay contact, and runs body on one session.
func runCallVerb(paths runtimePaths, verb, flagSetName, help string, args []string, body func(*relaySession, *verbEnvelope, planCallRequest) error) int {
	var opts verbOptions
	req := planCallRequest{Data: map[string]any{}}
	var sets []string
	dataFile := ""
	fs := flag.NewFlagSet(flagSetName, flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	fs.StringVar(&req.Match, "match", "", "resolve exactly one non-disabled entity of the service's domain by phrase")
	fs.StringVar(&req.Area, "area", "", "expand an area_id to the service domain's entities (previewed unless it resolves to exactly one entity of the service's domain)")
	fs.StringVar(&req.Device, "device", "", "expand a device_id to the service domain's entities (previewed unless it resolves to exactly one entity of the service's domain)")
	fs.Func("set", "service data field k=v, JSON-typed when it parses (repeatable)", func(v string) error { sets = append(sets, v); return nil })
	fs.StringVar(&dataFile, "data-file", "", "JSON object of service data; --set overrides its keys")
	fs.StringVar(&req.Skill, "skill", "", "the owning skill an owned service is called from, e.g. ha-nova:mqtt")
	fs.BoolVar(&req.Response, "response", false, "return the service's response data (result); implied for weather.get_forecasts, calendar.get_events, todo.get_items")
	positional, err := parseVerbArgs(fs, &opts, args, help)
	if err == nil {
		err = req.parse(positional, sets, dataFile)
	}
	if err != nil {
		return verbUsageError(verb, err)
	}
	if refusal := staticRefusal(req); refusal != nil { // needs no Relay at all
		env := verbEnvelope{SchemaVersion: 1, Verb: verb}
		exit := setVerbError(&env, refuse(&env, *refusal))
		if !printVerbEnvelope(env) {
			return verbExitError
		}
		return exit
	}
	return runReadVerb(paths, verb, opts, func(s *relaySession, env *verbEnvelope) error {
		return body(s, env, req)
	})
}

func (req *planCallRequest) parse(positional, sets []string, dataFile string) error {
	if len(positional) == 0 || !entityIDPattern.MatchString(positional[0]) {
		return errors.New("Usage: " + planCallUsage)
	}
	req.Service = positional[0]
	for _, id := range positional[1:] {
		if !entityIDPattern.MatchString(id) {
			return fmt.Errorf("%q is not an entity_id (domain.object_id)", id)
		}
		req.EntityIDs = append(req.EntityIDs, id)
	}
	if req.Match != "" && len(foldTokens(req.Match)) == 0 {
		return errors.New("--match needs at least one letter or digit")
	}
	if dataFile != "" {
		raw, err := os.ReadFile(dataFile)
		if err == nil {
			err = json.Unmarshal(raw, &req.Data)
		}
		if err != nil {
			return fmt.Errorf("--data-file must hold one JSON object: %v", err)
		}
		if req.Data == nil {
			return errors.New("--data-file must hold one JSON object, not null")
		}
	}
	for _, set := range sets {
		key, value, ok := strings.Cut(set, "=")
		if !ok || key == "" {
			return fmt.Errorf("--set %q must be key=value", set)
		}
		req.Data[key] = setValue(value)
	}
	return req.validate()
}

// setValue JSON-types a --set value (objects, lists, booleans, null), but a
// number only when it encodes back to exactly what was typed: "40" is a
// number, "1.10" (a version) or "1e3" stays the string typed.
func setValue(value string) any {
	var typed any
	if json.Unmarshal([]byte(value), &typed) != nil {
		return value
	}
	if _, isNumber := typed.(float64); isNumber {
		if back, err := json.Marshal(typed); err != nil || string(back) != value {
			return value
		}
	}
	return typed
}

// wantsResponse: --response, implied for the read-only response services.
func (req planCallRequest) wantsResponse() bool {
	return req.Response || readOnlyResponseServices[req.Service]
}

// validate holds for every request a plan may carry: parse checks fresh
// input with it, apply re-checks the stored request with it.
func (req planCallRequest) validate() error {
	if !entityIDPattern.MatchString(req.Service) {
		return fmt.Errorf("%q is not a domain.service", req.Service)
	}
	for _, id := range req.EntityIDs {
		if !entityIDPattern.MatchString(id) {
			return fmt.Errorf("%q is not an entity_id (domain.object_id)", id)
		}
	}
	if req.Data == nil {
		return errors.New("service data must be a JSON object")
	}
	for _, key := range []string{"entity_id", "area_id", "device_id", "target"} {
		if _, present := req.Data[key]; present {
			return fmt.Errorf("service data may not carry %q: pass targets as arguments, --area, or --device so the plan freezes them", key)
		}
	}
	return nil
}

func planCall(paths runtimePaths, s *relaySession, env *verbEnvelope, req planCallRequest) error {
	_, plan, err := computeCallPlan(s, env, req)
	if err != nil {
		return err
	}
	return savePlan(paths, plan, s.transport.Credential)
}

// computeCallPlan resolves, tiers, and builds the plan (not stored). plan
// call, call, and apply's recompute (I3) all run exactly this.
func computeCallPlan(s *relaySession, env *verbEnvelope, req planCallRequest) (*tierEngine, storedPlan, error) {
	e := newTierEngine(s)
	e.loadRegistry()
	// A Relay that never answered is an outage, not a scan gap: nothing is
	// planned or stored (exit 1). Failures after a first answer stay typed.
	if err := s.unreachableError(); err != nil {
		return nil, storedPlan{}, err
	}
	spec := e.serviceSpec(req.Service)
	if !spec.listed { // before any target or tier: an invented service never reaches a confirmation
		return nil, storedPlan{}, e.refuseUnlisted(env, req, spec)
	}
	targets, err := e.resolveTargets(req)
	if err != nil {
		return nil, storedPlan{}, err
	}
	env.Resolved = targets
	if failure := chatCallFailure(req, targets, e); failure != nil { // ha-nova serve: below Manage everything (mcp_serve_levels.go)
		return nil, storedPlan{}, failure
	}
	if spec.responseOnly && !req.wantsResponse() {
		return nil, storedPlan{}, refuse(env, tierReason{Code: reasonResponseOnly, Detail: "Home Assistant runs this service only with a response: call it again with --response"})
	}
	if e.targetRequired(req, spec) {
		return nil, storedPlan{}, refuse(env, tierReason{Code: reasonTargetRequired, Detail: "without a target this service acts on every entity of its domain; name the entities, or use --area/--device"})
	}
	if refusal := e.evaluate(req, targets); refusal != nil {
		return e, storedPlan{}, refuse(env, *refusal) // e: call's no-op check (noChangeCall)
	}
	plan, err := buildCallPlan(e, env, req, targets)
	return e, plan, err
}

// targetRequired: a service that takes a target, called with none, acts on
// EVERY entity of its domain in Home Assistant. A script run by its own name
// and scene.apply (entities in its data) name their targets themselves.
func (e *tierEngine) targetRequired(req planCallRequest, spec serviceSpec) bool {
	domain, name := splitID(req.Service)
	named := len(req.EntityIDs) > 0 || req.Match != "" || req.Area != "" || req.Device != ""
	return spec.target && !named && req.Service != "scene.apply" && (domain != "script" || scriptEntityServices[name])
}

// staticRefusal: owned services, hassio.*, floor/label selectors, and codes
// in the payload never reach a plan (spec §4, refused row).
func staticRefusal(req planCallRequest) *tierReason {
	domain, name := splitID(req.Service)
	_, floor := req.Data["floor_id"]
	_, label := req.Data["label_id"]
	_, code := req.Data["code"]
	switch {
	case ownerSkill(req.Service) != "" && req.Skill != ownerSkill(req.Service):
		return &tierReason{Code: reasonOwnedService, Skill: ownerSkill(req.Service)}
	case domain == "hassio":
		// Only the five App/host-control services run through `ha-nova call`,
		// and only from service-call (raw-routes.md → App And Host Control).
		// restore_full/restore_partial, addon_update, and every other hassio.*
		// stay refused whatever --skill says: their owning skills drive them
		// through their own flows, not a raw call.
		if !serviceCallHassio[name] || req.Skill != "ha-nova:service-call" {
			return &tierReason{Code: reasonHassio, Skill: hassioOwner(name)}
		}
	case floor || label:
		return &tierReason{Code: reasonSelector, Detail: "floor_id and label_id are unsupported; name entities, an area, or a device"}
	case code:
		return &tierReason{Code: reasonCodeRequired, Detail: "a code never goes into a payload; finish this in the Home Assistant UI"}
	}
	return nil
}

func refuse(env *verbEnvelope, r tierReason) *verbFailure {
	env.Tier = tierRefused.String()
	env.Reasons = []tierReason{r}
	next := "Finish this in the Home Assistant UI."
	skillRuns := r.Code == reasonOwnedService || (r.Code == reasonHassio && r.Skill == "ha-nova:service-call")
	switch {
	case r.Code == reasonResponseOnly:
		next = "Call it again with --response to get its data."
	case skillRuns:
		// The call runs through `ha-nova call` from its owning skill: name it.
		next = "Call it with --skill " + r.Skill + " (only from that skill)."
	case r.Skill != "":
		next = "Continue in " + r.Skill + "."
	}
	return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: "refused (" + r.Code + "); nothing was planned or sent", next: next}
}

// resolveTargets turns ids, --match, --area, and --device into the frozen
// leaf list (I6); groups expand to their leaves (membership_source:
// attributes). Only an ambiguous or missing NAMED target exits 5; a failed
// read is a typed gap, never an operational error.
func (e *tierEngine) resolveTargets(req planCallRequest) ([]verbTarget, error) {
	domain, _ := splitID(req.Service)
	var targets []verbTarget
	for _, id := range req.EntityIDs {
		targets = append(targets, verbTarget{EntityID: id, By: "exact"})
	}
	if req.Match != "" {
		filter := domain
		if domain == "homeassistant" || !coreDomains[domain] {
			filter = ""
		}
		found, err := findEntities(e.s, findQuery{phrase: req.Match, domain: filter, limit: 10, completeAliases: true})
		switch {
		case err != nil, found.Total == 0 && found.statesUnread != nil: // no match proves absence only with every state read
			e.readFailed("", "", "match")
		case found.Total == 1:
			if found.statesUnread != nil { // uniqueness unproven: unregistered look-alikes were not searched
				e.readFailed("", "", "match states")
			}
			match := found.Matches[0]
			match.By = "match"
			targets = append(targets, match)
		default:
			code, message := "AMBIGUOUS_TARGET", fmt.Sprintf("%q matches %d entities", req.Match, found.Total)
			if found.Total == 0 {
				code, message = "NOT_FOUND", fmt.Sprintf("no %s entity matches %q", domain, req.Match)
			}
			return nil, &verbFailure{exit: verbExitNotFound, code: code, message: message, candidates: found.Matches, next: "Name one entity_id, or a more specific --match."}
		}
	}
	var filtered []tierReason // what Home Assistant's own targeting skips (hidden, categorized, another area)
	expand := func(by, selector, area string, all []string) error {
		var ids []string
		for _, id := range all {
			if why := e.skipWhy(id, area, domain); why != "" {
				filtered = append(filtered, tierReason{Code: noteFilteredTargets, EntityID: id, Via: by, Detail: why})
			} else {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			failure := &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("no %s entity in %s %q", domain, by, selector)}
			for _, id := range all { // hidden, categorized, or assigned elsewhere: only naming it reaches it
				failure.candidates = append(failure.candidates, verbTarget{EntityID: id})
			}
			if len(all) > 0 {
				failure.next = "Room and device targets skip hidden, configuration, and diagnostic entities, as Home Assistant does; name the entity_id to act on one."
			}
			return failure
		}
		for _, id := range ids {
			targets = append(targets, verbTarget{EntityID: id, By: by})
		}
		return nil
	}
	if req.Area != "" {
		related, err := e.related("area", req.Area)
		if err != nil {
			e.readFailed("", "", "related area")
		} else if err := expand("area", req.Area, req.Area, filterDomain(related["entity"], domain)); err != nil {
			return nil, err
		}
	}
	if req.Device != "" && !e.regFailed { // a failed registry read is already a typed gap
		if err := expand("device", req.Device, "", e.familyEntities(req.Device, domain)); err != nil {
			return nil, err
		}
	}
	ids := make([]string, len(targets))
	for i, t := range targets {
		ids[i] = t.EntityID
	}
	e.prefetchStates(ids)
	out := []verbTarget{}
	seen := map[string]bool{}
	for _, t := range targets {
		if err := e.flatten(t, 0, &out, seen); err != nil {
			return nil, err
		}
	}
	for _, skip := range filtered { // named, or a group's leaf, it is still acted on
		if !seen[skip.EntityID] {
			seen[skip.EntityID] = true
			e.note(noteFilteredTargets)
			e.skipped = append(e.skipped, skip)
		}
	}
	return out, nil
}

func (e *tierEngine) flatten(t verbTarget, depth int, out *[]verbTarget, seen map[string]bool) error {
	if seen[t.EntityID] {
		return nil
	}
	seen[t.EntityID] = true
	named := t.By == "exact" || t.By == "match"
	st, err := e.state(t.EntityID)
	switch {
	case err != nil:
		e.readFailed(t.EntityID, "", "state") // kept: the user named or previewed it
	case st == nil && named:
		_, _, err := readEntityState(e.s, t.EntityID) // memoized 404 → NOT_FOUND with candidates
		return err
	case st == nil: // disabled or not loaded: nothing to act on, nothing to freeze
		e.note(noteSkippedTargets)
		e.skipped = append(e.skipped, tierReason{Code: noteSkippedTargets, EntityID: t.EntityID, Via: t.By})
		return nil
	}
	domain, _ := splitID(t.EntityID)
	if members := groupMembers(domain, st); members != nil {
		if depth+1 > tierMaxDepth {
			e.gap(reasonLimitCut, t.EntityID, "", fmt.Sprintf("deeper than %d", tierMaxDepth))
		} else {
			e.groups = append(e.groups, t.EntityID)
			e.prefetchStates(members)
			for _, member := range members {
				if err := e.flatten(verbTarget{EntityID: member, By: "group"}, depth+1, out, seen); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if len(*out) >= tierMaxActed {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d acted entities", tierMaxActed))
		return nil
	}
	t.Name = st.str("friendly_name")
	*out = append(*out, t)
	return nil
}

// singleExpansion: an area or device that resolves to exactly one entity of
// the service's own domain, with no group in between and no leaf skipped,
// acts on exactly what naming that entity would, so it is classified as that
// direct call (spec I5 exception). Expansion already drops what Home
// Assistant's own targeting skips (hidden, categorized, another area), and
// classify checks the device's access siblings on every path.
func (e *tierEngine) singleExpansion(req planCallRequest, targets []verbTarget) bool {
	domain, _ := splitID(req.Service)
	if len(targets) != 1 || (req.Area == "") == (req.Device == "") || len(e.groups) > 0 || slices.Contains(e.notes, noteSkippedTargets) {
		return false
	}
	entityDomain, _ := splitID(targets[0].EntityID)
	return entityDomain == domain && (targets[0].By == "area" || targets[0].By == "device")
}

// evaluate runs spec §2.2 steps 2–6 on the frozen targets; a non-nil
// result is a refusal that stops before any plan is stored.
func (e *tierEngine) evaluate(req planCallRequest, targets []verbTarget) *tierReason {
	domain, name := splitID(req.Service)
	if len(req.EntityIDs) > tierMaxExplicit {
		e.raise(tierPreview, tierReason{Code: reasonBatch, Detail: fmt.Sprintf("%d ids", len(req.EntityIDs))})
	}
	named := len(req.EntityIDs) > 0 || req.Match != "" || req.Area != "" || req.Device != ""
	if named && len(targets) == 0 {
		e.gap(reasonUnresolvedTarget, "", "", "no target resolved") // never a targetless call in disguise
	}
	single := e.singleExpansion(req, targets)
	var reads []func()
	for _, t := range targets {
		if t.By != "exact" && t.By != "match" && !single {
			e.raise(tierPreview, tierReason{Code: reasonExpanded, EntityID: t.EntityID, Via: t.By})
		}
		entityDomain, _ := splitID(t.EntityID)
		if code := directRefusal(effectiveService(req.Service, entityDomain), e.states[t.EntityID]); code != "" {
			return &tierReason{Code: code, EntityID: t.EntityID}
		}
		reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest("entity", t.EntityID)) })
		if device := e.reg[t.EntityID].di; device != "" { // before any long scan: the plan basis binds both (basisConsumers)
			reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest("device", device)) })
		}
	}
	prefetch(reads) // the consumer scan of every frozen target, warmed in parallel
	if e.liveEvents()["call_service"] > callServiceListenerBaseline {
		e.checkEvents("call_service", "")
	}
	switch {
	case req.Service == "scene.apply":
		e.raise(tierPreview, tierReason{Code: reasonStoredMembers, Via: req.Service})
		e.sceneMembers(req.Data["entities"], req.Service, 1)
	case domain == "script" && !scriptEntityServices[name]:
		e.visit(req.Service, "script.turn_on", req.Data, 0, "")
	case len(targets) == 0:
		level, code := classifyTargetless(req.Service, req.Data)
		e.raise(level, tierReason{Code: code})
	}
	for _, t := range targets {
		e.visit(t.EntityID, req.Service, req.Data, 0, "")
	}
	for _, group := range e.groups { // its consumers react to the leaves' change
		e.scanConsumers(group, 0, stateDelta{})
	}
	e.checkManagers()
	return nil
}
