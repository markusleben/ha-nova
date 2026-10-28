package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
)

// Raw plans (docs/work/2026-10-25-chat-parity-spec.md → Writes): the raw
// writes `ha-nova relay core|ws|files|backups` sends, as one plan of 1 to 20
// items the user confirms once. Every item passes the same guard as `relay`
// (guardRawRequestIn) at planning and again right before apply sends it
// (plan_raw_apply.go). Reads stay with `relay` and ha_read, except the few
// only a plan may send (rawPlanOnlyRead). The hash covers the items exactly;
// the only Home Assistant state in it is the energy lists a save replaces.

const planRawUsage = "ha-nova plan raw --file <requests.json>"

const planRawHelp = planRawUsage + `

Read-only: checks a JSON list of 1 to 20 raw Relay requests, each
{"endpoint": "core"|"ws"|"files"|"backups"|"supervisor", "payload": {…}},
the payload exactly as "ha-nova relay <endpoint>" sends it (core: {"method",
"path", "body"}; supervisor: {"method": "POST", "path", "data"}), and stores
one plan for "ha-nova apply". Apply checks each item
again right before sending it, sends them in order, and stops at the first
failure. Leading backups saves are the config snapshot: when one fails,
nothing else is sent and apply offers --no-snapshot. Reads go through
"ha-nova relay", except blueprint/import, camera/stream,
media_source/resolve_media, and a GET on a flow step.
Exit: 3 needs confirmation, 4 refused.`

const planRawMaxItems = 20 // the snapshot batch limit (skills/ha-nova/config-snapshots.md)

// rawRequest is one item: the payload exactly as `ha-nova relay <endpoint>`
// sends it.
type rawRequest struct {
	Endpoint string          `json:"endpoint"`
	Payload  json.RawMessage `json:"payload"`
	// Typed: the skill gates this step with a one-time code (skills/device
	// → Access Check); it only ever raises the item's tier (rawItemTier).
	Typed bool `json:"typed,omitempty"`
}

// rawPlanItem is one previewed item: Change is its plain line.
type rawPlanItem struct {
	Endpoint string `json:"endpoint"`
	Change   string `json:"change"`
	Tier     string `json:"tier"`
	verb     string // the phone's button and title (rawGoal)
	object   string
}

type rawPlanResult struct {
	Items []rawPlanItem `json:"items"`
}

func runPlanRaw(paths runtimePaths, args []string) int {
	var opts verbOptions
	file := ""
	fs := flag.NewFlagSet("plan raw", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	fs.StringVar(&file, "file", "", "JSON list of 1 to 20 {endpoint, payload} items")
	positional, err := parseVerbArgs(fs, &opts, args, planRawHelp)
	var items []rawRequest
	if err == nil && (len(positional) != 0 || file == "") {
		err = errors.New("Usage: " + planRawUsage)
	}
	if err == nil {
		items, err = readRawRequests(file)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		_, plan, err := computeRawPlan(s, env, items)
		if err != nil {
			return err
		}
		return savePlan(paths, plan, s.transport.Credential)
	})
}

// readRawRequests reads the --file list; each payload is stored compact, so
// the hash never depends on whitespace.
func readRawRequests(file string) ([]rawRequest, error) {
	raw, err := os.ReadFile(file)
	if err == nil {
		raw, err = strictJSONBytes(raw, "--file")
	}
	var items []rawRequest
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		err = dec.Decode(&items)
	}
	if err != nil {
		return nil, fmt.Errorf("--file must hold a JSON list of {endpoint, payload}: %v", err)
	}
	for i := range items {
		var compact bytes.Buffer
		if json.Compact(&compact, items[i].Payload) == nil {
			items[i].Payload = compact.Bytes()
		}
	}
	return items, validateRawRequests(items)
}

// validateRawRequests holds for every raw plan: planning checks fresh input
// with it, apply the stored items.
func validateRawRequests(items []rawRequest) error {
	if len(items) == 0 || len(items) > planRawMaxItems {
		return fmt.Errorf("a raw plan holds 1 to %d items", planRawMaxItems)
	}
	for i, item := range items {
		payload, ok := decodeObject(item.Payload)
		if !ok || !bytes.HasPrefix(bytes.TrimSpace(item.Payload), []byte("{")) {
			return fmt.Errorf("item %d: payload must be a JSON object", i+1)
		}
		switch item.Endpoint {
		case "ws", "files", "backups":
		case "core":
			method, _ := payload["method"].(string)
			target, _ := payload["path"].(string)
			for key := range payload {
				if key != "method" && key != "path" && key != "body" {
					return fmt.Errorf("item %d: a core payload holds method, path, and body only, not %q", i+1, key)
				}
			}
			if method != http.MethodGet && method != http.MethodPost && method != http.MethodDelete || !strings.HasPrefix(target, "/api/") || !plainRelayPath(target) {
				return fmt.Errorf("item %d: a core payload needs method GET, POST, or DELETE and a plain path under /api/ (no spaces, backslashes, or #)", i+1)
			}
		case "supervisor":
			if payload["method"] != http.MethodPost || len(payload) > 3 || len(payload) == 3 && payload["data"] == nil {
				return fmt.Errorf("item %d: a supervisor payload holds method POST, path, and data only", i+1)
			}
		default:
			return fmt.Errorf("item %d: endpoint must be core, ws, files, backups, or supervisor, not %q", i+1, item.Endpoint)
		}
	}
	return nil
}

// computeRawPlan checks every item (rawPlanRefusal, then the raw guard),
// tiers it (rawItemTier), and seals the plan. plan raw and apply's
// recompute run exactly this; one refused item refuses the whole plan.
func computeRawPlan(s *relaySession, env *verbEnvelope, items []rawRequest) (*tierEngine, storedPlan, error) {
	if err := validateRawRequests(items); err != nil {
		return nil, storedPlan{}, refused("PLAN_INVALID", err.Error())
	}
	e := newTierEngine(s)
	n := newRawNamer(s, e)
	result := rawPlanResult{}
	for i, item := range items {
		payload, _ := decodeObject(item.Payload)
		refusal := rawPlanRefusal(item.Endpoint, payload)
		if refusal == nil && item.Endpoint == "supervisor" && s.transport.Via == relayViaCloud { // before the user types a code
			refusal = errSupervisorViaCloud
		}
		var err error
		if refusal == nil {
			_, refusal, err = guardRawRequestIn(s, item.Endpoint, item.Payload)
		}
		if refusal == nil && err == nil && item.Endpoint == "backups" && payload["action"] == "delete" {
			refusal, err = n.snapshotListed(payload)
		}
		if err != nil {
			return nil, storedPlan{}, err
		}
		if refusal != nil {
			next := "Tell the user which step can't be done this way and why (the message says it). Plan the other steps without it only if they still want them."
			if errors.As(refusal, new(rawReadRefusal)) {
				next = "That step only reads: read it with ha_read (ha-nova relay on a computer), then plan the other steps."
			}
			return nil, storedPlan{}, &verbFailure{exit: verbExitRefused, code: "REFUSED", next: next,
				message: fmt.Sprintf("item %d: %s; nothing was planned or sent", i+1, strings.TrimSuffix(refusal.Error(), "; nothing was sent"))}
		}
		level, why := rawItemTier(n, item, payload)
		code := reasonRawWrite
		if level == tierTyped {
			code = reasonRawTyped
		}
		e.raise(level, tierReason{Code: code, Via: fmt.Sprintf("requests[%d]", i), Detail: why})
		var verb, object string
		if item.Endpoint == "supervisor" {
			verb, object = rawSupervisorLine(s, payload)
		} else {
			verb, object = rawItemLine(n, item.Endpoint, payload)
		}
		result.Items = append(result.Items, rawPlanItem{Endpoint: item.Endpoint, Change: verb + " " + object, Tier: level.String(), verb: verb, object: object})
	}
	for _, removes := range n.removing {
		if removes == "unreadable" {
			return nil, storedPlan{}, errors.New("cannot read the device and entity registries to tell what deleting the device removes, so nothing was planned or sent; try again")
		}
	}
	plan := storedPlan{Kind: "raw", RawRequests: items, Basis: n.basis()}
	plan, err := sealPlan(e, env, plan, items)
	env.Result = result
	if err == nil {
		env.Say, plan.Prompt = confirmationSay(e, env, plan, ""), confirmationPrompt(e, env, plan, "")
	}
	return e, plan, err
}

// rawPlanRefusal: what `relay` sends but a plan does not hold. A plan holds
// changes; reads go through `relay` (ha_read in chat apps), except
// rawPlanOnlyRead.
func rawPlanRefusal(endpoint string, payload map[string]any) error {
	read := rawReadRefusal(endpoint)
	action, _ := payload["action"].(string)
	switch endpoint {
	case "files":
		switch action {
		case "write_file", "delete_file":
			return nil
		case "list_dir", "read_file":
			return read
		}
		return errors.New("a plan writes a file with write_file or deletes one with delete_file")
	case "backups":
		switch action {
		case "save", "delete", "prune":
			return nil
		case "list", "load":
			return read
		}
		return errors.New("a plan saves, deletes, or prunes config snapshots (save, delete, prune)")
	case "ws":
		if _, typed := payload["type"]; !typed {
			return errors.New("a plan sends one WebSocket message with its type, never a collect_events envelope; watch events with ha-nova relay ws (ha_read in chat apps)")
		}
		if body, _ := json.Marshal(payload); rawRelayRead("ws", body) && !rawPlanOnlyRead("ws", payload) {
			return read
		}
	case "core":
		if payload["method"] == http.MethodGet && !rawPlanOnlyRead("core", payload) {
			return read
		}
	}
	return nil
}

// rawReadRefusal: a read in a plan; it belongs to relay and ha_read.
type rawReadRefusal string

func (endpoint rawReadRefusal) Error() string {
	return "it only reads: read it with ha-nova relay " + string(endpoint) + " (ha_read in chat apps), not in a plan"
}

// rawPlanOnlyRead: the reads only a plan sends, so the user sees them
// first: Home Assistant fetches a URL or hands out a live link
// (mcpPlannedReads), or a GET on a flow step re-runs it and can finish the
// flow (flowStepPath).
func rawPlanOnlyRead(endpoint string, payload map[string]any) bool {
	if endpoint == "ws" {
		wsType, _ := payload["type"].(string)
		return mcpPlannedReads[wsType]
	}
	target, _ := payload["path"].(string)
	return endpoint == "core" && payload["method"] == http.MethodGet && plainRelayPath(target) && !strings.ContainsAny(target, "%?") &&
		path.Clean(target) == target && flowStepPath.MatchString(target)
}

// rawSayWhat opens a raw plan's confirmation: one line per item, in order.
func rawSayWhat(env *verbEnvelope) (about, status string) {
	result, _ := env.Result.(rawPlanResult)
	if len(result.Items) == 1 {
		return sayAbout(result.Items[0].Change), sayNotRun
	}
	lines := []string{fmt.Sprintf("I'm about to do these %d steps in Home Assistant, in this order:", len(result.Items))}
	for i, item := range result.Items {
		lines = append(lines, fmt.Sprintf("%d. %s.", i+1, item.Change))
	}
	return strings.Join(lines, "\n"), sayNotRun
}

// rawGoal: the phone's verb and object, the item's own for one item.
func rawGoal(env *verbEnvelope) (verb, object string) {
	result, _ := env.Result.(rawPlanResult)
	if len(result.Items) == 1 {
		return result.Items[0].verb, result.Items[0].object
	}
	return "Confirm", fmt.Sprintf("%d steps in Home Assistant", len(result.Items))
}

// rawAsk: a typed raw plan's phone question. With several items the
// notification names the typed steps first, by number, then the reasons,
// then how many other steps come along, so a cut keeps what needs the tap.
func rawAsk(e *tierEngine, env *verbEnvelope, plan storedPlan) [3]string {
	verb, object := rawGoal(env)
	result, _ := env.Result.(rawPlanResult)
	var why []string
	for i, item := range result.Items {
		if len(result.Items) > 1 && item.Tier == tierTyped.String() {
			why = append(why, fmt.Sprintf("Step %d: %s", i+1, item.Change))
		}
	}
	others := len(result.Items) - len(why)
	reasons := map[string]bool{}
	for _, r := range env.Reasons {
		if text, typed := sayReason(e, env, plan, r); typed && text != "" && !reasons[text] {
			reasons[text] = true
			why = append(why, text)
		}
	}
	if len(result.Items) > 1 && others > 0 {
		why = append(why, "And "+plural(others, "other step"))
	}
	return [3]string{verb, object, strings.Join(why, ". ")}
}
