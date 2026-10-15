package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
)

const planCallUsage = `ha-nova plan call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>]`

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
}

func runPlanCommand(paths runtimePaths, args []string) int {
	if len(args) > 0 && args[0] == "call" {
		return runPlanCall(paths, args[1:])
	}
	if len(args) > 0 && (args[0] == "write" || args[0] == "delete" || args[0] == "revert") {
		return runPlanConfig(paths, args[0], args[1:])
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + planCallHelp)
		return verbExitOK
	}
	return verbUsageError("plan", errors.New("Usage: "+planCallUsage+" | "+planWriteUsage+" | "+planDeleteUsage+" | "+planRevertUsage))
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
		var typed any
		if json.Unmarshal([]byte(value), &typed) != nil {
			typed = value
		}
		req.Data[key] = typed
	}
	return req.validate()
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
	return savePlan(paths, plan)
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
	targets, err := e.resolveTargets(req)
	if err != nil {
		return nil, storedPlan{}, err
	}
	env.Resolved = targets
	spec := e.serviceSpec(req.Service)
	if spec.responseOnly { // a failed list read refuses nothing: HA then rejects the call (400) before running it
		return nil, storedPlan{}, refuse(env, tierReason{Code: reasonResponseOnly, Skill: "ha-nova:service-call", Detail: "Home Assistant runs this service only with return_response"})
	}
	if e.targetRequired(req, spec) {
		return nil, storedPlan{}, refuse(env, tierReason{Code: reasonTargetRequired, Detail: "without a target this service acts on every entity of its domain; name the entities, or use --area/--device"})
	}
	if refusal := e.evaluate(req, targets); refusal != nil {
		return nil, storedPlan{}, refuse(env, *refusal)
	}
	plan, err := buildCallPlan(e, env, req, targets)
	return e, plan, err
}

// serviceSpec is what /api/services says about one service: listed,
// takes a target, runs only with return_response. known is false when the
// list cannot be read.
type serviceSpec struct{ listed, target, responseOnly, known bool }

func (e *tierEngine) serviceSpec(service string) serviceSpec {
	status, body, err := e.s.coreGET("/api/services")
	var domains []struct {
		Domain   string `json:"domain"`
		Services map[string]struct {
			Target   json.RawMessage `json:"target"`
			Response *struct {
				Optional bool `json:"optional"`
			} `json:"response"`
		} `json:"services"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &domains) != nil {
		return serviceSpec{}
	}
	domain, name := splitID(service)
	for _, d := range domains {
		if spec, listed := d.Services[name]; d.Domain == domain && listed {
			return serviceSpec{listed: true, known: true, target: spec.Target != nil,
				responseOnly: spec.Response != nil && !spec.Response.Optional}
		}
	}
	return serviceSpec{known: true}
}

// targetRequired: a service that takes a target, called with none, acts on
// EVERY entity of its domain in Home Assistant. Without a readable service
// list it fails closed for any entity domain. A script run by its own name
// and scene.apply (entities in its data) name their targets themselves.
func (e *tierEngine) targetRequired(req planCallRequest, spec serviceSpec) bool {
	domain, name := splitID(req.Service)
	named := len(req.EntityIDs) > 0 || req.Match != "" || req.Area != "" || req.Device != ""
	switch {
	case named || req.Service == "scene.apply" || (domain == "script" && !scriptEntityServices[name]):
		return false
	case spec.listed:
		return spec.target
	case name == "reload":
		return false
	}
	if entityDomains[domain] {
		return true
	}
	for id := range e.reg {
		if strings.HasPrefix(id, domain+".") {
			return true
		}
	}
	return false
}

// staticRefusal: owned services, hassio.*, floor/label selectors, and codes
// in the payload never reach a plan (spec §4, refused row).
func staticRefusal(req planCallRequest) *tierReason {
	domain, name := splitID(req.Service)
	_, floor := req.Data["floor_id"]
	_, label := req.Data["label_id"]
	_, code := req.Data["code"]
	switch {
	case readOnlyResponseServices[req.Service]:
		return &tierReason{Code: reasonResponseOnly, Skill: "ha-nova:service-call", Detail: "a response service changes nothing; read it through the service-call read path"}
	case ownerSkill(req.Service) != "":
		return &tierReason{Code: reasonOwnedService, Skill: ownerSkill(req.Service)}
	case domain == "hassio":
		return &tierReason{Code: reasonHassio, Skill: hassioOwner(name)}
	case floor || label:
		return &tierReason{Code: reasonSelector, Detail: "floor_id and label_id are unsupported; name entities, an area, or a device"}
	case code:
		return &tierReason{Code: reasonCodeRequired, Detail: "a code never goes into a payload; finish this in the Home Assistant UI"}
	}
	return nil
}

func refuse(env *verbEnvelope, r tierReason) error {
	env.Tier = tierRefused.String()
	env.Reasons = []tierReason{r}
	next := "Finish this in the Home Assistant UI."
	if r.Skill != "" {
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
	expand := func(by, selector string, ids, skipped []string) error {
		if len(ids) == 0 {
			failure := &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("no %s entity in %s %q", domain, by, selector)}
			for _, id := range skipped { // hidden, categorized, or assigned elsewhere: only naming it reaches it
				failure.candidates = append(failure.candidates, verbTarget{EntityID: id})
			}
			if len(skipped) > 0 {
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
		all := filterDomain(related["entity"], domain)
		if err != nil {
			e.readFailed("", "", "related area")
		} else if err := expand("area", req.Area, e.primaryEntities(all, req.Area, domain), all); err != nil {
			return nil, err
		}
	}
	if req.Device != "" && !e.regFailed { // a failed registry read is already a typed gap
		all := e.familyEntities(req.Device, domain)
		if err := expand("device", req.Device, e.primaryEntities(all, "", domain), all); err != nil {
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
		if !slices.Contains(e.notes, noteSkippedTargets) {
			e.notes = append(e.notes, noteSkippedTargets)
		}
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
		e.scanConsumers(group, 0)
	}
	e.checkManagers()
	return nil
}
