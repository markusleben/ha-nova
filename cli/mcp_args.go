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
		"write":  {"kind", "domain", "id", "draft"},
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
