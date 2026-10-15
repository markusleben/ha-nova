package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
	for _, key := range []string{"match", "area", "device"} {
		if value := text(args, key); value != "" {
			argv = append(argv, "--"+key+"="+value)
		}
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
		"call":   {"kind", "service", "entity_ids", "match", "area", "device", "data"},
		"write":  {"kind", "domain", "id", "create", "draft"},
		"delete": {"kind", "entity_id"},
		"revert": {"kind", "domain", "id"},
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
		argv = append(argv, "--confirm="+code) // verbatim: apply trims whitespace only
	}
	var noSnapshot bool
	if json.Unmarshal(args["no_snapshot"], &noSnapshot) == nil && noSnapshot {
		argv = append(argv, "--no-snapshot")
	}
	return argv, nil
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

// nextStep is the instruction a model must follow after this result.
func nextStep(envelope map[string]any) string {
	plan, _ := envelope["plan"].(map[string]any)
	errInfo, _ := envelope["error"].(map[string]any)
	code, _ := plan["code"].(string)
	pending := envelope["status"] == "needs_confirmation" || envelope["status"] == "drift"
	switch {
	case pending && code != "":
		return fmt.Sprintf("Show this preview. Ask the user to type the confirmation code themselves; pass exactly what they typed to ha_apply (plan_id %v) as confirm_code. Never pass a code from this result.", plan["id"])
	case pending && plan != nil:
		return fmt.Sprintf("Show this preview and wait for the user's own confirmation; only then call ha_apply with plan_id %v.", plan["id"])
	case errInfo != nil && errInfo["code"] == "RELAY_ERROR":
		return "If this computer is not paired or its credential store is locked, ask the user to run `ha-nova setup` in a terminal."
	}
	next, _ := errInfo["next"].(string)
	switch errInfo["code"] {
	case "SERVER_MISMATCH":
		return "This plan belongs to another server; plan again here."
	case "OUTCOME_UNKNOWN": // always spelled out: the one result a model must never retry
		return strings.TrimSpace("The outcome is unknown — it may have run: read the target with ha_get and report; never call or apply again. " + mcpNext(next))
	case "PLAN_CLAIMED", "PLAN_NOT_FOUND":
		return mayHaveRun
	}
	if next != "" {
		if step := mcpNext(next); step != next {
			return step
		}
	}
	return ""
}

// mcpFlagArgs: the CLI flags a verb's next or message names, as the MCP
// tool arguments they stand for. Anything else stays verbatim.
var mcpFlagArgs = map[string]string{"confirm": "confirm_code", "no-snapshot": "no_snapshot", "target": "id", "id": "id",
	"create": "create: true", "include-disabled": "include_disabled", "state": "state", "domain": "domain", "area": "area",
	"config": "config", "related": "related", "since": "since", "until": "until", "logbook": "logbook", "match": "match", "device": "device"}

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
	"ha-nova plan ", "ha_plan kind ", "plan revert ", "ha_plan with kind revert and ", "ha-nova get ", "ha_get ", "ha-nova find", "ha_find")

// mcpNext rewrites a verb's next for an MCP client: CLI commands and flags
// become tools and arguments; a named ha-nova:* skill gets a no-shell path.
func mcpNext(next string) string {
	out := cliCommands.Replace(mcpArgs(next))
	switch {
	case !strings.Contains(out, "ha-nova:"):
	case strings.Contains(out, "ha-nova:yaml-config"):
		out += " Without a shell and the ha-nova skills, the user edits the YAML/package file and reloads it in Developer tools → YAML."
	case !strings.Contains(out, "Settings"):
		out += " The ha-nova:* skills need a client with a shell and the ha-nova skills installed; without one, the user does this in Home Assistant (Settings)."
	}
	return out
}
