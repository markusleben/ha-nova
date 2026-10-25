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
	"skill":      prop("string", "the owning skill, e.g. ha-nova:mqtt, required for an owned service (mqtt.publish, notify.*, todo.*, calendar.*, media_player.*, camera.*, tts.*, recorder.purge*, update.*, logger.set_level, conversation.process, hassio.addon_*/host_*)"),
	"response":   prop("boolean", "return the service's response data in result (for response services like weather.get_forecasts)"),
}

var mcpTools = []mcpTool{mcpSkillTool,
	{name: "ha_find", title: "Find devices", description: "Find entities by phrase (case-, accent-, alias-aware) and/or by domain, area, or current state — e.g. which lights are on: domain light, state on. With state, rows marked group: true count their members again — report them separately; entities in state unavailable are a separate could-not-tell bucket (ask again with state unavailable and with state unknown). " + mcpAnswerRules, readOnly: true, idempotent: true,
		schema: object(nil, map[string]any{"phrase": prop("string", "words to match (optional with domain, area, or state)"), "domain": prop("string", "only this domain"),
			"area": prop("string", "only this area_id"), "state": prop("string", "only entities currently in this state, e.g. on"),
			"limit": prop("integer", "1-200 rows (default 20)"), "include_disabled": prop("boolean", "include disabled entities"),
			"aliases": prop("boolean", "also match aliases when names already match (after a rejected name match)")}),
		argv: findArgv, run: runFindCommand},
	{name: "ha_get", title: "Read a device", description: "Read one entity's state, optionally its stored config and relations. managed: yaml means the config is defined in YAML or a package: never edit or delete it through ha_plan; it is changed in those files.", readOnly: true, idempotent: true,
		schema: object([]string{"entity_id"}, map[string]any{"entity_id": prop("string", "exact entity_id"),
			"config": prop("boolean", "also read the stored automation/script/scene config"), "related": prop("boolean", "also read search/related"),
			"references": prop("boolean", "also scan automations, scripts, dashboards, energy, and state attributes for the entity, its device, and the device's other entities (includes related)")}),
		argv: func(a map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
			return flagsThen(a, []string{"config", "related", "references"}, nil, "entity_id")
		}, run: runGetCommand},
	{name: "ha_history", title: "Read history", description: "Summarize an entity's history in a window of at most 7 days (each bound a duration ago of at most 7d or an RFC3339 time).", readOnly: true, idempotent: true,
		schema: object([]string{"entity_id"}, map[string]any{"entity_id": prop("string", "exact entity_id"),
			"since": prop("string", "window start: a duration ago (90m, 24h, 7d) or an RFC3339 time"),
			"until": prop("string", "window end: a duration ago or an RFC3339 time (default now)"), "logbook": prop("boolean", "also read the logbook")}),
		argv: func(a map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
			return flagsThen(a, []string{"logbook"}, []string{"since", "until"}, "entity_id")
		}, run: runHistoryCommand},
	{name: "ha_map", title: "Map the home", description: "Exact, computed home facts — never count by hand: areas/floors with device and entity counts, automations/scripts/scenes/helpers, integrations, unavailable entities, low batteries. section gives one full list; entity gives where-used (automations, scripts, scenes, groups, helpers, dashboard cards; not_checked names what was not searched); first_look gives the onboarding summary. " + mcpAnswerRules, readOnly: true, idempotent: true,
		schema: object(nil, map[string]any{"section": map[string]any{"type": "string", "enum": []string{"areas", "integrations", "automations", "unavailable", "batteries"}},
			"entity": prop("string", "exact entity_id: where it is used"), "first_look": prop("boolean", "the onboarding first look"),
			"limit": prop("integer", "1-200 rows for section or entity")}),
		argv: mapArgv, run: runMapCommand},
	{name: "ha_trace", title: "Read automation runs", description: "Latest trace, trace list, or one trace of an automation or script.", readOnly: true, idempotent: true,
		schema: object([]string{"action", "entity_id"}, map[string]any{"action": map[string]any{"type": "string", "enum": []string{"latest", "list", "get"}},
			"entity_id": prop("string", "automation.x or script.x"), "run_id": prop("string", "for action get")}),
		argv: traceArgvChecked, run: runTraceCommand},
	mcpReadTool,
	{name: "ha_plan", title: "Plan a change", description: "Plan without executing: kind call (a service call), write (automation/script config), delete, revert, restore (a deleted automation/script from the snapshot its delete result named), or raw (the requests to Home Assistant's own API a skill makes, such as registry, helper, scene, and integration setup changes, files, config snapshots; sent in order by ha_apply). Returns a preview; a high-consequence plan needs a confirmation code the user types (no code is returned where their app asks for it in a form). coverage.notes are informational: mention one only when it raised the tier or the user asks — except EXPANDED_TARGET_WITHOUT_STATE: always tell the user in plain words which coverage.skipped entries with that code were skipped (not reporting).",
		readOnly: true,
		schema: object([]string{"kind"}, merged(callProperties, map[string]any{
			"kind": map[string]any{"type": "string", "enum": []string{"call", "write", "delete", "revert", "restore", "raw"}},
			"requests": map[string]any{"type": "array", "minItems": 1, "maxItems": planRawMaxItems,
				"description": "raw: 1 to 20 requests in the order they run (config snapshots first, then what they protect); reads go to ha_read, except blueprint/import, camera/stream, media_source/resolve_media, and a flow step GET, which only a plan sends",
				"items": object([]string{"endpoint", "payload"}, map[string]any{
					"endpoint": map[string]any{"type": "string", "enum": []string{"core", "ws", "files", "backups"}},
					"payload":  prop("object", "core: {method, path, body} of the REST API; ws: the WebSocket message; files: {action: write_file or delete_file, path, content}; backups: {action: save, delete, or prune, ...}"),
					"typed":    prop("boolean", "true where the skill takes the one-time code for this step and the plan would only preview it; never lowers a tier")})},
			"domain":    map[string]any{"type": "string", "enum": []string{"automation", "script"}},
			"id":        prop("string", "write: config id to update/create; revert: target config id; restore: the deleted item's config id"),
			"snapshot":  prop("string", "restore: the snapshot file the delete result named"),
			"create":    prop("boolean", "write: a new item — refused if the id already exists, never an overwrite"),
			"draft":     prop("object", "write: the full config (key order is kept)"),
			"entity_id": prop("string", "delete: automation.x or script.x")})),
		argv: planArgv, run: runPlanCommand},
	{name: "ha_call", title: "Control a device", description: "Call a service. Ordinary calls run now and are verified; no_change: already so, nothing sent (relay say); anything else returns needs_confirmation with a plan for ha_apply. A call that timed out or returned no result is outcome unknown: read with ha_get; never call again. coverage.notes are informational: mention one only when it raised the tier or the user asks — except EXPANDED_TARGET_WITHOUT_STATE: always tell the user in plain words which coverage.skipped entries with that code were skipped (not reporting).",
		schema: object([]string{"service"}, callProperties), argv: callArgv, run: runCallCommand},
	{name: "ha_apply", title: "Run a confirmed change", description: "Run a change the user confirmed, once: a preview plan after the user agreed to its preview; a plan with a one-time code from HA NOVA only with the code the user typed for it — or, where a result has no code (a confirmation window on their computer, a notification on their phone, or a form in their app asks them), without confirm_code (a passed code is ignored). no_change: already so, nothing sent (relay say). A call that timed out or returned no result is outcome unknown: read with ha_get; never call again.",
		destructive: true,
		schema: object([]string{"plan_id"}, map[string]any{"plan_id": prop("string", "the plan_id from the preview (p_ + 12 hex)"),
			"confirm_code": prop("string", "the four-digit one-time code exactly as the user typed it in chat (ignored where a window, phone, or form asks)"), "no_snapshot": prop("boolean", "deletes only, after a failed snapshot and the user's choice")}),
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

func (srv *mcpServer) callTool(req rpcRequest) (map[string]any, *rpcError) {
	var params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
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
		if refusal := srv.chatRefusal(tool); refusal != nil { // ha-nova serve: the owner's read-only switch
			return refusal, nil
		}
		if refusal := srv.chatRawRefusal(tool.name, params.Arguments); refusal != nil { // ha-nova serve: raw plans for administrators
			return refusal, nil
		}
		if tool.name == mcpSkillTool.name { // offline: no verb, no lock
			return srv.callSkill(params.Arguments), nil
		}
		answered := ""
		if tool.name == "ha_apply" && req.forms { // only the human's answer in a form confirms here
			if stop, rpcErr := srv.confirmByForm(req, params.Arguments, &answered); stop != nil || rpcErr != nil {
				return stop, rpcErr
			}
		}
		temp := &mcpTempFiles{}
		defer temp.remove()
		argv, err := tool.argv(params.Arguments, temp)
		if err != nil {
			return invalidArguments(tool.name, err), nil
		}
		if waited := srv.chatApply(tool.name, params.Arguments, argv); waited != nil { // ha-nova serve: a phone plan
			return waited, nil
		}
		return srv.runVerb(tool.name, answered, req.forms, func() int { return tool.run(srv.paths, pinServer(tool.name, argv, srv.pin)) }), nil
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
	case "ha_find", "ha_get", "ha_history", "ha_map", "ha_call", "ha_apply":
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

// toolResult: the envelope as text and as structuredContent — the same object, so
// a client that reads only one still gets the guidance (next_step, say). isError for
// failures, not needs_confirmation. told (nil = no memory) keeps each update
// notice's hint to once per session. forms: the client shows forms, so no code.
func toolResult(name, out, errOut string, exit int, told map[string]bool, forms bool) map[string]any {
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
	next := nextStep(envelope)
	if own, _ := envelope["next_step"].(string); next == "" && own != "" { // the verb's own step, in tool terms
		next = mcpNext(own)
	}
	if forms {
		next = formsNext(envelope, next)
	}
	images := imageContent(envelope)          // before the text: an image is never text
	text := plainResult(envelope, next, told) // say, next_step, and answer_rule (mcp_plain.go)
	return map[string]any{"content": append([]map[string]any{{"type": "text", "text": string(text)}}, images...), "structuredContent": envelope,
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

func mapArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	argv, _ := flagsThen(args, []string{"first_look"}, []string{"section", "entity"}, "")
	if raw, present := args["limit"]; present {
		var n int
		_ = json.Unmarshal(raw, &n)
		argv = append(argv, "--limit="+strconv.Itoa(n))
	}
	return argv, nil
}

func findArgv(args map[string]json.RawMessage, _ *mcpTempFiles) ([]string, error) {
	argv, _ := flagsThen(args, []string{"include_disabled", "aliases"}, []string{"domain", "area", "state"}, "")
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
