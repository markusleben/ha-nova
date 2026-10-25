package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ha_read reads what `ha-nova relay` reads (docs/work/2026-10-25-chat-parity-spec.md
// → Reads): every Relay request passes the same guard as the CLI
// (guardRawRequest), then mcpReadRefusal narrows it to requests that change
// nothing and keep data in the house, since "Always allow" sends them unseen.
// snapshot and diff run the CLI verbs offline. Values reach runMCPRead only
// as --flag=value, or after "--" for the CLI verb, never as a bare flag.

const (
	mcpReadMaxResult = 64 << 10 // a bigger result fills the chat: filter it with jq
	mcpCameraMax     = 5 << 20  // Claude refuses a bigger image
	mcpMessageMax    = 1 << 10  // an error message carries at most this much of Home Assistant's text
	mcpReasonMax     = 2 << 10  // Home Assistant's reason for an HTTP error
	mcpPlanIt        = "plan it with ha_plan kind raw (the user sees it first), then ha_apply"
)

// mcpJQDeadline bounds a jq filter: ha_read runs under mcpVerbLock, so a
// filter that never ends would stop every tool (a variable for tests).
var mcpJQDeadline = 5 * time.Second

var mcpReadArgs = map[string][]string{
	"core":     {"endpoint", "path", "method", "payload", "jq"},
	"camera":   {"endpoint", "entity_id"},
	"ws":       {"endpoint", "payload", "jq"},
	"files":    {"endpoint", "payload", "jq"},
	"backups":  {"endpoint", "payload", "jq"},
	"snapshot": {"endpoint", "action", "list", "target", "domain", "against", "jq"},
	"diff":     {"endpoint", "before", "after", "jq"},
}

// mcpPlannedReads: reads the CLI sends raw that ha_read leaves to a plan the
// user sees first: Home Assistant fetches any URL, or hands out a live link.
var mcpPlannedReads = map[string]bool{"blueprint/import": true, "camera/stream": true, "media_source/resolve_media": true}

// mcpCameraTypes: the image types chat apps show.
var mcpCameraTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

// mcpTooLargeHint: a filter that fits each endpoint's result.
var mcpTooLargeHint = map[string]string{
	"core":     "`.data.body | length`, `.data.body | keys`, or for a text body such as /api/error_log `.data.body | split(\"\\n\") | .[-200:]`",
	"ws":       "`.data | length` or `[.data[] | select(.entity_id | startswith(\"light.\")) | .entity_id]`",
	"backups":  "`.data | length` or `.data[0:20]`",
	"files":    "`.data.content[0:60000]` (read_file) or `.data.entries[0:200]` (list_dir)",
	"snapshot": "`.[0:20]` for a list, or `{target_id, domain}` for a checkpoint",
	"diff":     "`.[0:50]`",
}

var mcpReadTool = mcpTool{name: "ha_read", title: "Advanced read", readOnly: true, idempotent: true,
	description: "Reads Home Assistant's own API, read only: a path of its REST API (https://developers.home-assistant.io/docs/api/rest/) or a message of its WebSocket API (https://developers.home-assistant.io/docs/api/websocket/), a camera's current image, files under /config (when file access is on in the NOVA Relay App), and HA NOVA's config snapshots and undo checkpoints; it also compares two configs. Only requests on HA NOVA's read list are accepted. A result over 64 KiB is refused: filter it with jq.",
	schema: object([]string{"endpoint"}, map[string]any{
		"endpoint": map[string]any{"type": "string", "enum": []string{"core", "camera", "ws", "files", "backups", "snapshot", "diff"},
			"description": "core: REST API; camera: a camera's image; ws: WebSocket API; files: files under /config; backups: config snapshots; snapshot: undo checkpoints; diff: two configs"},
		"path":      prop("string", "core: a REST API path under /api/"),
		"method":    map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "description": "core: GET (default); POST only for /api/template and /api/config/core/check_config"},
		"payload":   prop("object", "core POST: the body; ws: the message, e.g. {\"type\":\"search/related\",...}, or {message, collect_events}; files: {action: list_dir or read_file, path}; backups: {action: list or load, ...}"),
		"entity_id": prop("string", "camera: camera.x"),
		"action":    map[string]any{"type": "string", "enum": []string{"show", "verify"}, "description": "snapshot: show or verify"},
		"list":      prop("boolean", "snapshot show: list the checkpoints"),
		"target":    prop("string", "snapshot: a checkpoint's target_id (default: the newest)"),
		"domain":    prop("string", "snapshot: the target's domain"),
		"against":   prop("object", "snapshot verify: the live config"),
		"before":    prop("object", "diff: the config before"),
		"after":     prop("object", "diff: the config after"),
		"jq":        prop("string", "a jq filter on the result, as ha-nova relay --jq: core, ws, files, backups give the Relay's {ok, data}; snapshot show a checkpoint (list: a list); verify \"match\" or \"drift\"; diff a list of table rows. Several outputs come back as a list"),
	}),
	argv: readArgv, run: runMCPRead}

// verbImage is a camera image for the MCP layer; toolResult moves it out of
// the envelope into image content.
type verbImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// imageContent moves a camera image out of the envelope into MCP image content.
func imageContent(envelope map[string]any) []map[string]any {
	image, _ := envelope["image"].(map[string]any)
	if image == nil {
		return nil
	}
	delete(envelope, "image")
	return []map[string]any{{"type": "image", "data": image["data"], "mimeType": image["mimeType"]}}
}

func readArgv(args map[string]json.RawMessage, temp *mcpTempFiles) ([]string, error) {
	endpoint := text(args, "endpoint")
	for key := range args {
		if !containsText(mcpReadArgs[endpoint], key) {
			return nil, fmt.Errorf("argument %q does not apply to endpoint %s", key, endpoint)
		}
	}
	argv := []string{endpoint}
	if _, set := args["jq"]; set {
		filter := text(args, "jq")
		if strings.TrimSpace(filter) == "" {
			return nil, errors.New("jq needs a filter")
		}
		if _, err := compileJQFilter(filter); err != nil {
			return nil, err
		}
		argv = append(argv, "--jq="+filter)
	}
	file := func(key string) (string, error) {
		raw, set := args[key]
		if !set {
			return "", fmt.Errorf("endpoint %s needs %s", endpoint, key)
		}
		path, err := temp.write(raw)
		return "--" + key + "=" + path, err
	}
	switch endpoint {
	case "core":
		method := text(args, "method")
		if method == "" {
			method = http.MethodGet
		}
		if _, set := args["payload"]; set && method != http.MethodPost {
			return nil, errors.New("payload applies to a core POST only")
		}
		if text(args, "path") == "" {
			return nil, errors.New("endpoint core needs path")
		}
		body, err := buildRelayRequestBody("core", relayRequestOptions{Method: method, Path: text(args, "path")}, args["payload"])
		return append(argv, "--body="+string(body)), err
	case "camera":
		id := text(args, "entity_id")
		if !entityIDPattern.MatchString(id) || !strings.HasPrefix(id, "camera.") {
			return nil, fmt.Errorf("%q is not a camera's entity_id (camera.x)", id)
		}
		body, err := json.Marshal(map[string]string{"method": http.MethodGet, "path": "/api/camera_proxy/" + id})
		return append(argv, "--body="+string(body)), err
	case "ws", "files", "backups":
		payload, set := args["payload"]
		if !set {
			return nil, fmt.Errorf("endpoint %s needs payload", endpoint)
		}
		return append(argv, "--body="+string(payload)), nil
	case "snapshot":
		action := text(args, "action")
		if action == "" {
			return nil, errors.New("endpoint snapshot needs action show or verify")
		}
		argv = append(argv, "--action="+action, "--")
		var list bool
		if json.Unmarshal(args["list"], &list) == nil && list {
			argv = append(argv, "--list")
		}
		for _, key := range []string{"target", "domain"} {
			if _, set := args[key]; set {
				argv = append(argv, "--"+key+"="+text(args, key))
			}
		}
		_, hasAgainst := args["against"]
		switch {
		case action == "show" && hasAgainst:
			return nil, errors.New("against applies to action verify")
		case action == "verify" && list:
			return nil, errors.New("list applies to action show")
		case action == "verify":
			against, err := file("against")
			return append(argv, against), err
		}
		return argv, nil
	}
	before, err := file("before")
	if err != nil {
		return nil, err
	}
	after, err := file("after")
	return append(argv, "--", before, after), err
}

// runMCPRead is ha_read. argv: the endpoint, its --flag=value options, and
// after "--" the arguments of the CLI verb behind snapshot and diff.
func runMCPRead(paths runtimePaths, argv []string) int {
	fs := flag.NewFlagSet("ha_read", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	body, filter, action := fs.String("body", "", ""), fs.String("jq", "", ""), fs.String("action", "", "")
	if err := fs.Parse(argv[1:]); err != nil {
		return verbUsageError("read", err)
	}
	var run jqRun
	if *filter != "" {
		program, err := compileJQFilter(*filter)
		if err != nil {
			return verbUsageError("read", err)
		}
		run = inProcessJQ(program)
		if chatCall != nil { // ha-nova serve: a filter's memory stays out of the bridge (mcp_read_jq.go)
			run = childJQ(*filter)
		}
	}
	endpoint := argv[0]
	switch endpoint {
	case "snapshot":
		return runOfflineRead(paths, endpoint, run, func() (json.RawMessage, error) { return readSnapshot(paths, *action, fs.Args()) })
	case "diff":
		return runOfflineRead(paths, endpoint, run, func() (json.RawMessage, error) { return readDiff(paths, fs.Args()) })
	}
	relayEndpoint := endpoint
	if endpoint == "camera" {
		relayEndpoint = "core"
	}
	send, refusal, err := guardRawRequest(paths, verbOptions{}, relayEndpoint, []byte(*body))
	if refusal == nil && err == nil {
		refusal = mcpReadRefusal(endpoint, send)
	}
	if refusal != nil {
		err = &verbFailure{exit: verbExitRefused, code: "REFUSED", message: refusal.Error()}
	}
	if err != nil {
		env := verbEnvelope{SchemaVersion: 1, Verb: "read"}
		exit := setVerbError(&env, err)
		printVerbEnvelope(env)
		return exit
	}
	return runReadVerb(paths, "read", verbOptions{}, func(s *relaySession, env *verbEnvelope) error {
		if refusal := mcpDemoRefusal(activeServerIsDemo(), endpoint, send); refusal != nil { // known once the session loaded the profile
			return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: refusal.Error()}
		}
		data, err := s.send(relayEndpoint, send)
		var call *relayCallError
		if errors.As(err, &call) { // Home Assistant's or the Relay's answer, not a pairing problem
			return &verbFailure{exit: verbExitError, code: "READ_FAILED", message: mcpMessage(call.Error(), mcpMessageMax),
				next: "Nothing was changed. Tell the user what the message says, or correct the request and read again."}
		}
		if err != nil {
			return err
		}
		if endpoint == "camera" { // a frame is no JSON to redact; an error reason is redacted below
			return readCamera(env, data)
		}
		envelope := redactRawResponse(relayEndpoint, append(append([]byte(`{"ok":true,"data":`), data...), '}'))
		if failure := upstreamFailure(relayEndpoint, envelope); failure != nil {
			return failure
		}
		result, err := readResult(endpoint, envelope, run)
		env.Result = result
		return err
	})
}

// mcpReadRefusal: what the guard lets `ha-nova relay` send but ha_read does
// not (body has passed guardRawRequest).
func mcpReadRefusal(endpoint string, body []byte) error {
	payload, _ := decodeObject(body)
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		path, _ := payload["path"].(string)
		switch {
		case method == http.MethodPost && (path == "/api/template" || path == "/api/config/core/check_config"):
			return nil
		case method != http.MethodGet:
			return fmt.Errorf("ha_read sends POST only to /api/template and /api/config/core/check_config; to change something, %s; nothing was sent", mcpPlanIt)
		case !readablePath(path):
			return errors.New("ha_read core reads plain paths under /api/, but no webhooks, backup downloads, Supervisor requests, media proxies, or streams (a camera's image: endpoint camera); nothing was sent")
		case flowStepPath.MatchString(strings.SplitN(path, "?", 2)[0]):
			return fmt.Errorf("a GET on a flow step can finish the flow: %s; nothing was sent", mcpPlanIt)
		case !rawRelayRead("core", body):
			return errors.New("send the plain path (no %-escapes or dot segments); nothing was sent")
		}
	case "ws":
		message, _ := payload["message"].(map[string]any)
		for _, wsType := range []any{payload["type"], message["type"]} {
			if name, _ := wsType.(string); mcpPlannedReads[name] {
				return fmt.Errorf("%s fetches a URL or hands out a live link: %s; nothing was sent", name, mcpPlanIt)
			}
		}
		if !rawRelayRead("ws", body) {
			return fmt.Errorf("ha_read only reads; to change something, %s; nothing was sent", mcpPlanIt)
		}
	case "files":
		if action, _ := payload["action"].(string); action != "list_dir" && action != "read_file" {
			return fmt.Errorf("ha_read reads files with list_dir and read_file only; to write or delete one, %s; nothing was sent", mcpPlanIt)
		}
	case "backups":
		if action, _ := payload["action"].(string); action != "list" && action != "load" {
			return fmt.Errorf("ha_read lists and loads config snapshots only; to save, delete, or prune one, %s; nothing was sent", mcpPlanIt)
		}
	}
	return nil
}

// mcpDemoRefusal: on the hosted demo every visitor is the same Home
// Assistant administrator, and its log holds other visitors' failed sign-ins.
func mcpDemoRefusal(demo bool, endpoint string, body []byte) error {
	if !demo {
		return nil
	}
	payload, _ := decodeObject(body)
	message, _ := payload["message"].(map[string]any)
	path, _ := payload["path"].(string)
	if endpoint == "core" && strings.SplitN(path, "?", 2)[0] == "/api/error_log" || endpoint == "ws" && (payload["type"] == "system_log/list" || message["type"] == "system_log/list") {
		return errors.New("the demo house's log holds other visitors' sign-in attempts, so ha_read does not read it there; nothing was sent")
	}
	return nil
}
