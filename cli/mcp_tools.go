package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// The MCP tools are thin argv adapters over the CLI verbs: the same parsing,
// validation, plans, codes, and envelopes, run in process with their output
// captured (never on the protocol's stdout).

type mcpTool struct {
	name, title, description          string
	readOnly, destructive, idempotent bool
	schema                            map[string]any
	argv                              func(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error)
	run                               func(runtimePaths, []string) int
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func object(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

var callProperties = map[string]any{
	"service":    prop("string", "domain.service, e.g. light.turn_off"),
	"entity_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "exact entity_ids"},
	"match":      prop("string", "a phrase that must match exactly one entity of the service's domain"),
	"area":       prop("string", "an area_id: expands to its entities (previewed unless it resolves to exactly one entity of the service's domain)"),
	"device":     prop("string", "a device_id: expands to its entities (previewed unless it resolves to exactly one entity of the service's domain)"),
	"data":       prop("object", "service data; never entity_id/area_id/device_id/target"),
}

var mcpTools = []mcpTool{mcpSkillTool,
	{name: "ha_find", title: "Find entities", description: "Find entities by phrase (case-, accent-, alias-aware) and/or by domain, area, or current state — e.g. which lights are on: domain light, state on. With state, rows marked group: true count their members again — report them separately; entities in state unavailable are a separate could-not-tell bucket (ask again with state unavailable and with state unknown).", readOnly: true, idempotent: true,
		schema: object(nil, map[string]any{"phrase": prop("string", "words to match (optional with domain, area, or state)"), "domain": prop("string", "only this domain"),
			"area": prop("string", "only this area_id"), "state": prop("string", "only entities currently in this state, e.g. on"),
			"limit": prop("integer", "1-200 rows (default 20)"), "include_disabled": prop("boolean", "include disabled entities")}),
		argv: findArgv, run: runFindCommand},
	{name: "ha_get", title: "Read an entity", description: "Read one entity's state, optionally its stored config and relations. managed: yaml means the config is defined in YAML or a package: never edit or delete it through ha_plan; it is changed in those files.", readOnly: true, idempotent: true,
		schema: object([]string{"entity_id"}, map[string]any{"entity_id": prop("string", "exact entity_id"),
			"config": prop("boolean", "also read the stored automation/script/scene config"), "related": prop("boolean", "also read search/related")}),
		argv: func(a map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
			return flagsThen(a, []string{"config", "related"}, nil, "entity_id")
		}, run: runGetCommand},
	{name: "ha_history", title: "Read history", description: "Summarize an entity's history in a window of at most 7 days (each bound a duration ago of at most 7d or an RFC3339 time).", readOnly: true, idempotent: true,
		schema: object([]string{"entity_id"}, map[string]any{"entity_id": prop("string", "exact entity_id"),
			"since": prop("string", "window start: a duration ago (90m, 24h, 7d) or an RFC3339 time"),
			"until": prop("string", "window end: a duration ago or an RFC3339 time (default now)"), "logbook": prop("boolean", "also read the logbook")}),
		argv: func(a map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
			return flagsThen(a, []string{"logbook"}, []string{"since", "until"}, "entity_id")
		}, run: runHistoryCommand},
	{name: "ha_trace", title: "Read automation traces", description: "Latest trace, trace list, or one trace of an automation or script.", readOnly: true, idempotent: true,
		schema: object([]string{"action", "entity_id"}, map[string]any{"action": map[string]any{"type": "string", "enum": []string{"latest", "list", "get"}},
			"entity_id": prop("string", "automation.x or script.x"), "run_id": prop("string", "for action get")}),
		argv: traceArgvChecked, run: runTraceCommand},
	{name: "ha_read", title: "Raw read", description: "A raw read-only Home Assistant request: core GET /api/... or a WS message from the read allowlist.", readOnly: true, idempotent: true,
		schema: object([]string{"endpoint"}, map[string]any{"endpoint": map[string]any{"type": "string", "enum": []string{"core", "ws"}},
			"path": prop("string", "core: /api/... path (GET only)"), "payload": prop("object", "ws: the message, e.g. {\"type\":\"search/related\",...}")}),
		argv: readArgv, run: runMCPRead},
	{name: "ha_plan", title: "Plan a change", description: "Plan without executing: kind call (a service call), write (automation/script config), delete, or revert. Returns a preview and, for high-consequence plans, a confirmation code the user must type. coverage.notes are informational: mention one only when it raised the tier or the user asks — except EXPANDED_TARGET_WITHOUT_STATE: always tell the user in plain words which entities were skipped (not reporting).",
		readOnly: true,
		schema: object([]string{"kind"}, merged(callProperties, map[string]any{
			"kind":      map[string]any{"type": "string", "enum": []string{"call", "write", "delete", "revert"}},
			"domain":    map[string]any{"type": "string", "enum": []string{"automation", "script"}},
			"id":        prop("string", "write: config id to update/create; revert: target config id"),
			"create":    prop("boolean", "write: a new item — refused if the id already exists, never an overwrite"),
			"draft":     prop("object", "write: the full config (key order is kept)"),
			"entity_id": prop("string", "delete: automation.x or script.x")})),
		argv: planArgv, run: runPlanCommand},
	{name: "ha_call", title: "Control a device", description: "Call a service. Ordinary calls run now and are verified; anything else returns needs_confirmation with a plan for ha_apply. A call that timed out or returned no result is outcome unknown: read with ha_get; never call again. coverage.notes are informational: mention one only when it raised the tier or the user asks — except EXPANDED_TARGET_WITHOUT_STATE: always tell the user in plain words which entities were skipped (not reporting).",
		schema: object([]string{"service"}, callProperties), argv: callArgv, run: runCallCommand},
	{name: "ha_apply", title: "Apply a confirmed plan", description: "Execute a stored plan once after the user confirmed its preview. confirm_code must be exactly what the user typed. A call that timed out or returned no result is outcome unknown: read with ha_get; never call again.",
		destructive: true,
		schema: object([]string{"plan_id"}, map[string]any{"plan_id": prop("string", "p_ + 12 hex"),
			"confirm_code": prop("string", "the code exactly as the user typed it"), "no_snapshot": prop("boolean", "deletes only, after a failed snapshot and the user's choice")}),
		argv: applyArgv, run: runApplyCommand},
}

func merged(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func mcpToolList(paths runtimePaths) []map[string]any {
	var tools []map[string]any
	for _, t := range mcpTools {
		if t.name == mcpSkillTool.name {
			t.description = skillDescription(paths)
		}
		tools = append(tools, map[string]any{"name": t.name, "title": t.title, "description": t.description, "inputSchema": t.schema,
			"annotations": map[string]any{"title": t.title, "readOnlyHint": t.readOnly, "destructiveHint": t.destructive,
				"idempotentHint": t.idempotent, "openWorldHint": false}})
	}
	return tools
}

// mcpVerbLock serializes tool runs: verbs share process-wide state (stdout
// capture, server selection).
var mcpVerbLock sync.Mutex

func (srv *mcpServer) callTool(raw json.RawMessage) (map[string]any, *rpcError) {
	var params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "tools/call needs name and arguments"}
	}
	for _, tool := range mcpTools {
		if tool.name != params.Name {
			continue
		}
		// Invalid arguments are a tool error the model can correct, not a
		// protocol error.
		if err := checkArguments(tool.schema, params.Arguments); err != nil {
			return invalidArguments(tool.name, err), nil
		}
		if tool.name == mcpSkillTool.name { // offline: no verb, no lock
			return srv.callSkill(params.Arguments), nil
		}
		temp := &mcpTempFiles{}
		defer temp.remove()
		argv, err := tool.argv(params.Arguments, temp)
		if err != nil {
			return invalidArguments(tool.name, err), nil
		}
		mcpVerbLock.Lock()
		defer mcpVerbLock.Unlock()
		// Verbs set process-wide server selection (apply selects the plan's
		// server); every call starts from the pin and leaves nothing behind.
		override, active := serverSelectionOverride, activeServerProfile()
		defer func() { setServerSelectionOverride(override); setActiveServerProfile(active) }()
		setServerSelectionOverride(srv.pin)
		traceRelayVersion.Store("")
		out, errOut, exit := captureVerb(func() int { return tool.run(srv.paths, pinServer(tool.name, argv, srv.pin)) })
		if tool.name == "ha_trace" { // trace prints its own object: add the verbs' notices
			out = withNotices(out, verbNotices(srv.paths, traceRelayVersion.Load().(string)), activeServerProfile())
		}
		return toolResult(tool.name, out, errOut, exit, srv.told), nil
	}
	return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown tool: " + params.Name}
}

// pinServer passes the mcp --server pin as the verb's own --server flag,
// so apply refuses another server's plan (SERVER_MISMATCH) exactly as the
// CLI does. trace and ha_read read the selection override instead.
func pinServer(tool string, argv []string, pin string) []string {
	if pin == "" {
		return argv
	}
	flag := "--server=" + pin
	switch tool {
	case "ha_find", "ha_get", "ha_history", "ha_call", "ha_apply":
		return append([]string{flag}, argv...)
	case "ha_plan": // after the subcommand
		return append([]string{argv[0], flag}, argv[1:]...)
	}
	return argv
}

func invalidArguments(name string, err error) map[string]any {
	envelope := map[string]any{"schema_version": 1, "verb": strings.TrimPrefix(name, "ha_"), "status": "error",
		"error": map[string]any{"code": "INVALID_ARGUMENTS", "message": err.Error() + "; nothing was sent"}}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope, "isError": true}
}

// withNotices adds notices and the server to a JSON object output.
func withNotices(out string, notices []verbNotice, server string) string {
	var object map[string]any
	if len(notices) == 0 || json.Unmarshal([]byte(out), &object) != nil || object == nil {
		return out
	}
	object["notices"], object["server"] = notices, server
	b, err := json.Marshal(object)
	if err != nil {
		return out
	}
	return string(b)
}

// toolResult: the envelope as text and as structuredContent — the same
// object, so a client that reads only one of them still gets the guidance
// (next_step). isError for failures, not for needs_confirmation. told
// (nil = no memory) keeps each update notice's hint to once per session.
func toolResult(name, out, errOut string, exit int, told map[string]bool) map[string]any {
	var envelope map[string]any
	// A verb envelope carries schema_version; anything else (trace prints
	// plain JSON, failures print to stderr) is wrapped into one.
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &envelope) != nil || envelope["schema_version"] == nil {
		var payload any
		status := "ok"
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil || exit != 0 {
			status, payload = "error", nil
		}
		envelope = map[string]any{"schema_version": 1, "verb": strings.TrimPrefix(name, "ha_"), "status": status, "result": payload}
		if status == "error" {
			envelope["error"] = map[string]any{"code": "TOOL_ERROR", "message": strings.TrimSpace(errOut)}
		}
	}
	if errInfo, _ := envelope["error"].(map[string]any); errInfo != nil {
		if message, _ := errInfo["message"].(string); message != "" {
			errInfo["message"] = mcpArgs(message)
		}
	}
	if step := strings.TrimSpace(nextStep(envelope) + " " + noticeHints(envelope, told)); step != "" {
		envelope["next_step"] = step
	}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope,
		"isError": exit != 0 && exit != verbExitConfirmation}
}

// captureVerb runs a verb with stdout and stderr redirected into pipes,
// always restores them (a panic becomes a tool error), and closes every pipe.
func captureVerb(run func() int) (out, errOut string, exit int) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return "", "cannot capture verb output", verbExitError
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_, _ = outR.Close(), outW.Close()
		return "", "cannot capture verb output", verbExitError
	}
	read := func(r *os.File, into *string, done chan<- struct{}) {
		b, _ := io.ReadAll(r)
		_ = r.Close()
		*into = string(b)
		close(done)
	}
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go read(outR, &out, outDone)
	go read(errR, &errOut, errDone)
	stdout, stderr := os.Stdout, os.Stderr
	var crash string
	func() {
		defer func() {
			if p := recover(); p != nil {
				exit, crash = verbExitError, fmt.Sprint("internal error: ", p)
			}
		}()
		os.Stdout, os.Stderr = outW, errW
		exit = run()
	}()
	os.Stdout, os.Stderr = stdout, stderr
	_, _ = outW.Close(), errW.Close()
	<-outDone
	<-errDone
	if crash != "" {
		out, errOut = "", strings.TrimSpace(errOut+"\n"+crash)
	}
	return out, errOut, exit
}

// mcpTempFiles holds 0600 files for drafts and data passed by file.
type mcpTempFiles struct{ paths []string }

func (t *mcpTempFiles) write(raw json.RawMessage) (string, error) {
	f, err := os.CreateTemp("", "ha-nova-mcp-*.json")
	if err != nil {
		return "", err
	}
	t.paths = append(t.paths, f.Name())
	_, err = f.Write(raw)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return filepath.Clean(f.Name()), err
}

func (t *mcpTempFiles) remove() {
	for _, path := range t.paths {
		_ = os.Remove(path)
	}
}

// checkArguments enforces the schema's required keys, known keys, and
// primitive types, so no argument can turn into a CLI flag.
func checkArguments(schema map[string]any, args map[string]json.RawMessage) error {
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, key := range required {
		if _, present := args[key]; !present {
			return fmt.Errorf("missing required argument %q", key)
		}
	}
	for key, raw := range args {
		spec, known := properties[key].(map[string]any)
		if !known {
			return fmt.Errorf("unknown argument %q", key)
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("argument %q is not JSON", key)
		}
		ok := false
		switch spec["type"] {
		case "string":
			text, isText := value.(string)
			enum, hasEnum := spec["enum"].([]string)
			ok = isText && (!hasEnum || containsText(enum, text))
		case "integer":
			n, isNumber := value.(float64)
			ok = isNumber && n == float64(int(n))
		case "boolean":
			_, ok = value.(bool)
		case "object":
			_, ok = value.(map[string]any)
		case "array":
			items, isList := value.([]any)
			ok = isList
			for _, item := range items {
				_, isText := item.(string)
				ok = ok && isText
			}
		}
		if !ok {
			return fmt.Errorf("argument %q has the wrong type", key)
		}
	}
	return nil
}

func containsText(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func text(args map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(args[key], &s)
	return s
}

// flagsThen builds `--flag=value…` (values can never be read as flags), then
// `--` and the positional argument.
func flagsThen(args map[string]json.RawMessage, bools, strs []string, positional string) ([]string, error) {
	var argv []string
	for _, key := range bools {
		var on bool
		if json.Unmarshal(args[key], &on) == nil && on {
			argv = append(argv, "--"+strings.ReplaceAll(key, "_", "-"))
		}
	}
	for _, key := range strs {
		if value := text(args, key); value != "" {
			argv = append(argv, "--"+strings.ReplaceAll(key, "_", "-")+"="+value)
		}
	}
	if positional != "" {
		argv = append(argv, "--", text(args, positional))
	}
	return argv, nil
}

func findArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	argv, _ := flagsThen(args, []string{"include_disabled"}, []string{"domain", "area", "state"}, "")
	if raw, present := args["limit"]; present {
		var n int
		_ = json.Unmarshal(raw, &n)
		argv = append(argv, "--limit="+strconv.Itoa(n))
	}
	if phrase := text(args, "phrase"); phrase != "" {
		argv = append(argv, "--", phrase)
	}
	return argv, nil
}

func readArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	switch text(args, "endpoint") {
	case "core":
		path := text(args, "path")
		if !mcpReadablePath(path) {
			return nil, fmt.Errorf("ha_read core reads only /api/states, /api/config, /api/services, /api/events, /api/history/period, /api/logbook, and stored automation/script/scene configs; %q is not one of them", path)
		}
		return []string{"core", path}, nil
	default:
		return []string{"ws", string(args["payload"])}, nil
	}
}
