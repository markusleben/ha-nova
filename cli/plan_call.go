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

const planCallUsage = `ha-nova plan call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>]`

const planCallHelp = planCallUsage + `

Read-only: resolves the targets, computes the confirmation tier, and stores
the plan for 15 minutes. This release never executes a plan.
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
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + planCallHelp)
		return verbExitOK
	}
	return verbUsageError("plan", errors.New("Usage: "+planCallUsage+" (plan write, delete, and revert are not available yet)"))
}

func runPlanCall(paths runtimePaths, args []string) int {
	var opts verbOptions
	req := planCallRequest{Data: map[string]any{}}
	var sets []string
	dataFile := ""
	fs := flag.NewFlagSet("plan call", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	fs.StringVar(&req.Match, "match", "", "resolve exactly one non-disabled entity of the service's domain by phrase")
	fs.StringVar(&req.Area, "area", "", "expand an area_id to the service domain's entities (always previewed)")
	fs.StringVar(&req.Device, "device", "", "expand a device_id to the service domain's entities (always previewed)")
	fs.Func("set", "service data field k=v, JSON-typed when it parses (repeatable)", func(v string) error { sets = append(sets, v); return nil })
	fs.StringVar(&dataFile, "data-file", "", "JSON object of service data; --set overrides its keys")
	positional, err := parseVerbArgs(fs, &opts, args, planCallHelp)
	if err == nil {
		err = req.parse(positional, sets, dataFile)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	if refusal := staticRefusal(req); refusal != nil { // needs no Relay at all
		env := verbEnvelope{SchemaVersion: 1, Verb: "plan"}
		exit := setVerbError(&env, refuse(&env, *refusal))
		if !printVerbEnvelope(env) {
			return verbExitError
		}
		return exit
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		return planCall(paths, s, env, req)
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
	for _, key := range []string{"entity_id", "area_id", "device_id", "target"} {
		if _, present := req.Data[key]; present {
			return fmt.Errorf("service data may not carry %q: pass targets as arguments, --area, or --device so the plan freezes them", key)
		}
	}
	return nil
}

func planCall(paths runtimePaths, s *relaySession, env *verbEnvelope, req planCallRequest) error {
	e := newTierEngine(s)
	e.loadRegistry()
	targets, err := e.resolveTargets(req)
	if err != nil {
		return err
	}
	env.Resolved = targets
	if refusal := e.evaluate(req, targets); refusal != nil {
		return refuse(env, *refusal)
	}
	return storeCallPlan(paths, e, env, req, targets)
}

// staticRefusal: owned services, hassio.*, floor/label selectors, and codes
// in the payload never reach a plan (spec §4, refused row).
func staticRefusal(req planCallRequest) *tierReason {
	domain, name := splitID(req.Service)
	_, floor := req.Data["floor_id"]
	_, label := req.Data["label_id"]
	_, code := req.Data["code"]
	switch {
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
		case err != nil:
			e.readFailed("", "", "match")
		case found.Total == 1:
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
	expand := func(by, selector string, ids []string) error {
		if len(ids) == 0 {
			return &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("no %s entity in %s %q", domain, by, selector)}
		}
		for _, id := range ids {
			targets = append(targets, verbTarget{EntityID: id, By: by})
		}
		return nil
	}
	if req.Area != "" {
		if related, err := e.related("area", req.Area); err != nil {
			e.readFailed("", "", "related area")
		} else if err := expand("area", req.Area, filterDomain(related["entity"], domain)); err != nil {
			return nil, err
		}
	}
	if req.Device != "" && !e.regFailed { // a failed registry read is already a typed gap
		if err := expand("device", req.Device, e.deviceEntities(req.Device, domain)); err != nil {
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
	var reads []func()
	for _, t := range targets {
		if t.By != "exact" && t.By != "match" {
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
