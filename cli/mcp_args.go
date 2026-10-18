package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// argv builders for the planning and writing tools. Every free-text value
// goes in as --flag=value or after a pattern check, so no MCP argument can
// become a CLI flag; data and drafts travel as 0600 temp files with their
// bytes (and key order) intact.

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

var mcpConfigReadPattern = regexp.MustCompile(`^/api/config/(automation|script|scene)/config/[A-Za-z0-9_-]+$`)

// mcpReadablePath is ha_read's explicit allowlist (the read surfaces the
// skills use), on top of the shared readablePath deny list.
func mcpReadablePath(raw string) bool {
	if !readablePath(raw) {
		return false
	}
	pathPart, _, _ := strings.Cut(raw, "?")
	decoded, _ := url.PathUnescape(pathPart)
	switch {
	case decoded == "/api/states", decoded == "/api/config", decoded == "/api/services", decoded == "/api/events":
		return true
	case strings.HasPrefix(decoded, "/api/states/"), strings.HasPrefix(decoded, "/api/history/period"), strings.HasPrefix(decoded, "/api/logbook"):
		return true
	}
	return mcpConfigReadPattern.MatchString(decoded)
}

func traceArgvChecked(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error) {
	entityID, runID := text(args, "entity_id"), text(args, "run_id")
	switch {
	case !entityIDPattern.MatchString(entityID):
		return nil, fmt.Errorf("%q is not an entity_id", entityID)
	case text(args, "action") == "get" && !runIDPattern.MatchString(runID):
		return nil, errors.New("action get needs a run_id")
	}
	argv := []string{text(args, "action"), entityID}
	if text(args, "action") == "get" {
		argv = append(argv, runID)
	}
	return append(argv, "--json"), nil
}

// targetArgv: service, exact ids (pattern-checked), selectors, data file.
func targetArgv(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error) {
	service := text(args, "service")
	if !entityIDPattern.MatchString(service) {
		return nil, fmt.Errorf("%q is not a domain.service", service)
	}
	argv := []string{service}
	var ids []string
	_ = json.Unmarshal(args["entity_ids"], &ids)
	for _, id := range ids {
		if !entityIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%q is not an entity_id", id)
		}
		argv = append(argv, id)
	}
	for _, key := range []string{"match", "area", "device", "skill"} {
		if value := text(args, key); value != "" {
			argv = append(argv, "--"+key+"="+value)
		}
	}
	var response bool
	if json.Unmarshal(args["response"], &response) == nil && response {
		argv = append(argv, "--response")
	}
	if data, present := args["data"]; present {
		path, err := temp.write(data)
		if err != nil {
			return nil, err
		}
		argv = append(argv, "--data-file="+path)
	}
	return argv, nil
}

func callArgv(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error) {
	return targetArgv(args, temp)
}

func planArgv(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error) {
	kind := text(args, "kind")
	allowed := map[string][]string{
		"call":    {"kind", "service", "entity_ids", "match", "area", "device", "data", "skill", "response"},
		"write":   {"kind", "domain", "id", "create", "draft"},
		"delete":  {"kind", "entity_id"},
		"revert":  {"kind", "domain", "id"},
		"restore": {"kind", "snapshot", "id"},
	}[kind]
	for key := range args {
		if !containsText(allowed, key) {
			return nil, fmt.Errorf("argument %q does not apply to kind %s", key, kind)
		}
	}
	switch kind {
	case "call":
		argv, err := targetArgv(args, temp)
		return append([]string{"call"}, argv...), err
	case "write":
		draft, present := args["draft"]
		if !present {
			return nil, errors.New("kind write needs draft")
		}
		path, err := temp.write(draft)
		if err != nil {
			return nil, err
		}
		argv := []string{"write", "--domain=" + text(args, "domain"), "--file=" + path}
		if id := text(args, "id"); id != "" {
			argv = append(argv, "--id="+id)
		}
		var create bool
		if json.Unmarshal(args["create"], &create) == nil && create {
			argv = append(argv, "--create")
		}
		return argv, nil
	case "delete":
		entityID := text(args, "entity_id")
		if !entityIDPattern.MatchString(entityID) {
			return nil, fmt.Errorf("%q is not an entity_id", entityID)
		}
		return []string{"delete", entityID}, nil
	case "restore":
		snapshot := text(args, "snapshot")
		if restoreDomain(snapshot) == "" {
			return nil, fmt.Errorf("%q is not an automations/ or scripts/ snapshot file", snapshot)
		}
		argv := []string{"restore", "--snapshot=" + snapshot}
		if id := text(args, "id"); id != "" {
			argv = append(argv, "--id="+id)
		}
		return argv, nil
	}
	argv := []string{"revert"}
	if id := text(args, "id"); id != "" {
		argv = append(argv, "--target="+id)
	}
	if domain := text(args, "domain"); domain != "" {
		argv = append(argv, "--domain="+domain)
	}
	return argv, nil
}

func applyArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	argv := []string{"--plan=" + text(args, "plan_id")}
	if code := text(args, "confirm_code"); code != "" {
		argv = append(argv, "--confirm="+code) // verbatim: apply reads it (typedCode)
	}
	var noSnapshot bool
	if json.Unmarshal(args["no_snapshot"], &noSnapshot) == nil && noSnapshot {
		argv = append(argv, "--no-snapshot")
	}
	return argv, nil
}

func readArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	shell := " needs a client with a shell and the HA NOVA CLI: skip that evidence and say it was not checked"
	switch text(args, "endpoint") {
	case "core":
		path := text(args, "path")
		if !mcpReadablePath(path) {
			return nil, fmt.Errorf("ha_read core reads only /api/states, /api/config, /api/services, /api/events, /api/history/period, /api/logbook, and stored automation/script/scene configs; %q%s", path, shell)
		}
		return []string{"core", path}, nil
	default:
		var payload struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(args["payload"], &payload)
		if !readOnlyWSTypes[payload.Type] { // refused here, not as a Relay error that reads like a pairing problem
			types := make([]string, 0, len(readOnlyWSTypes))
			for wsType := range readOnlyWSTypes {
				types = append(types, wsType)
			}
			sort.Strings(types)
			return nil, fmt.Errorf("ha_read ws reads only %s; %q%s", strings.Join(types, ", "), payload.Type, shell)
		}
		return []string{"ws", string(args["payload"])}, nil
	}
}

// runMCPRead is ha_read: one raw read through the session's read allowlist
// (I1) — core GET /api/... or an allowlisted WS message.
func runMCPRead(paths runtimePaths, argv []string) int {
	return runReadVerb(paths, "read", verbOptions{}, func(s *relaySession, env *verbEnvelope) error {
		if argv[0] == "core" {
			status, body, err := s.coreGET(argv[1])
			if err != nil {
				return err
			}
			env.Result = map[string]any{"status": status, "body": body}
			if status >= http.StatusBadRequest {
				return &verbFailure{exit: verbExitError, code: "UPSTREAM_" + strconv.Itoa(status), message: fmt.Sprintf("Home Assistant answered HTTP %d", status)}
			}
			return nil
		}
		var payload map[string]any
		if json.Unmarshal([]byte(argv[1]), &payload) != nil {
			return &verbFailure{exit: verbExitError, code: "USAGE", message: "payload must be a WS message object"}
		}
		data, err := s.ws(payload)
		env.Result = data
		return err
	})
}

// nextStep is the instruction a model must follow after this result
// (formsStep adjusts it where the client shows confirmation forms).
func nextStep(envelope map[string]any) string {
	plan, _ := envelope["plan"].(map[string]any)
	errInfo, _ := envelope["error"].(map[string]any)
	code, _ := plan["code"].(string)
	pending := envelope["status"] == "needs_confirmation" || envelope["status"] == "drift"
	say := ""
	if text, _ := envelope["say"].(string); text != "" {
		say = " Tell the user `say` as the confirmation text (in their language; names, state values, and the one-time code exactly as printed) and add no other reason."
	}
	switch {
	case pending && code != "":
		return fmt.Sprintf("Show this preview. Ask the user to type the one-time code from HA NOVA themselves; pass exactly what they typed to ha_apply (plan_id %v) as confirm_code. Never pass a code from this result.", plan["id"]) + say
	case pending && (plan["channel"] == channelWindow || plan["channel"] == channelPhone):
		where := map[bool]string{true: "a confirmation window on their computer", false: "a notification on their phone"}[plan["channel"] == channelWindow]
		return fmt.Sprintf("Show this preview. When the user has asked for this change and your workflow has reached this step (a skill's order comes first), call ha_apply with plan_id %v and no confirm_code: %s asks them. Never ask for a code in chat.", plan["id"], where) + say
	case pending && plan != nil:
		return fmt.Sprintf("Show this preview and wait for the user's own confirmation; only then call ha_apply with plan_id %v.", plan["id"]) + say
	case errInfo != nil && errInfo["code"] == "RELAY_ERROR":
		return "If this computer is not paired or its credential store is locked, ask the user to run `ha-nova setup` in a terminal."
	}
	if restore, _ := envelope["restore"].(string); restore != "" && errInfo == nil {
		restore, _, _ = strings.Cut(restore, " --server ") // an MCP session is pinned to one server
		return "If the user asks to undo this delete: " + mcpNext(restore) + "."
	}
	next, _ := errInfo["next"].(string)
	switch errInfo["code"] {
	case "SERVER_MISMATCH":
		return "This plan belongs to another server; plan again here."
	case "OUTCOME_UNKNOWN": // always spelled out: the one result a model must never retry
		return strings.TrimSpace("The outcome is unknown — it may have run: read the target with ha_get and report; never call or apply again. " + mcpNext(next))
	case "PLAN_CLAIMED", "PLAN_NOT_FOUND":
		return mayHaveRun
	case "CONFIRM_DECLINED", "CONFIRM_CANCELLED", "CONFIRM_UNAVAILABLE", "CONFIRM_IN_FORM", "CONFIRM_IN_WINDOW", "CONFIRM_ON_PHONE", "CONFIRM_REJECTED": // a form's, window's, or phone's outcome
		return mcpNext(next)
	}
	if next != "" {
		if step := mcpNext(next); step != next {
			return step
		}
	}
	return ""
}

// noticeHints: the plain-language step for each update notice this session
// (one MCP process) has not been told yet; emitting the hint counts as told.
func noticeHints(envelope map[string]any, told map[string]bool) string {
	notices, _ := envelope["notices"].([]any)
	server, _ := envelope["server"].(string)
	var hints []string
	for _, raw := range notices {
		notice, _ := raw.(map[string]any)
		kind, _ := notice["kind"].(string)
		hint, known := mcpNoticeHints[kind]
		key := kind // update: one HA NOVA install for every profile
		if kind == "relay_outdated" {
			key += "\x00" + server // each profile has its own Relay
		}
		if !known || told[key] {
			continue
		}
		if told != nil {
			told[key] = true
		}
		hints = append(hints, hint)
	}
	return strings.Join(hints, " ")
}

var mcpNoticeHints = map[string]string{
	"relay_outdated": "After answering, tell the user once: the NOVA Relay App needs an update (Home Assistant → Settings → Apps → NOVA Relay); a standalone Relay needs its new container image pulled.",
	"update":         "After answering, tell the user once: a newer HA NOVA is available (see notices); they update it in a terminal as the notice says, then start a new session.",
	"draft":          "Before any preview: a draft notice names a trigger that may never fire; add the named in-between state to from: and plan again.",
}

// mcpFlagArgs: the CLI flags a verb's next or message names, as the MCP
// tool arguments they stand for. Anything else stays verbatim.
var mcpFlagArgs = map[string]string{"confirm": "confirm_code", "no-snapshot": "no_snapshot", "snapshot": "snapshot", "target": "id", "id": "id",
	"create": "create: true", "include-disabled": "include_disabled", "state": "state", "domain": "domain", "area": "area",
	"config": "config", "related": "related", "since": "since", "until": "until", "logbook": "logbook", "match": "match", "device": "device",
	"references": "references", "aliases": "aliases"}

var cliFlag = regexp.MustCompile("(?:^|[\\s`(/])--([a-z][a-z-]*)")

// mcpArgs rewrites standalone known flag tokens only — never text inside an
// identifier (foo--bar) or an unknown flag; messages carry HA and user text.
func mcpArgs(text string) string {
	var out strings.Builder
	last := 0
	for _, m := range cliFlag.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2]-2, m[3]
		arg, known := mcpFlagArgs[text[m[2]:m[3]]]
		if !known || (end < len(text) && !strings.ContainsRune(" \t\n=`).,;:/", rune(text[end]))) {
			continue
		}
		out.WriteString(text[last:start] + arg)
		last = end
	}
	return out.String() + text[last:]
}

// cliCommands maps CLI commands in a verb's next to MCP tools; longest first.
var cliCommands = strings.NewReplacer("ha-nova plan call|write|delete|revert", "ha_plan",
	"ha-nova plan ", "ha_plan kind ", "plan revert ", "ha_plan with kind revert and ", "ha-nova get ", "ha_get ", "ha-nova find", "ha_find",
	"ha-nova apply --plan ", "ha_apply with plan_id ")

// mcpNext rewrites a verb's next for an MCP client: CLI commands and flags
// become tools and arguments; a named ha-nova:* skill gets a no-shell path.
func mcpNext(next string) string {
	out := cliCommands.Replace(mcpArgs(next))
	switch {
	case !strings.Contains(out, "ha-nova:"):
	case strings.Contains(out, "ha-nova:yaml-config"):
		out += " Without a shell client, the user edits the YAML/package file and reloads it in Developer tools → YAML."
	case !strings.Contains(out, "Settings"):
		out += " That skill's writes need a client with a shell and the HA NOVA CLI (ha_skill serves it for its read steps); without one, the user does this in Home Assistant (Settings)."
	}
	return out
}
