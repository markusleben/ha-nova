package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type traceLatestOutput struct {
	SchemaVersion int             `json:"schema_version"`
	OK            bool            `json:"ok"`
	EntityID      string          `json:"entity_id"`
	Domain        string          `json:"domain"`
	UniqueID      string          `json:"unique_id,omitempty"`
	RunID         string          `json:"run_id,omitempty"`
	Timestamp     string          `json:"timestamp,omitempty"`
	LastStep      string          `json:"last_step,omitempty"`
	Error         string          `json:"error,omitempty"`
	Trace         json.RawMessage `json:"trace,omitempty"`
}

type traceListOutput struct {
	SchemaVersion int              `json:"schema_version"`
	OK            bool             `json:"ok"`
	EntityID      string           `json:"entity_id"`
	Domain        string           `json:"domain"`
	UniqueID      string           `json:"unique_id,omitempty"`
	Count         int              `json:"count"`
	Traces        []traceListEntry `json:"traces,omitempty"`
	Error         string           `json:"error,omitempty"`
}

type traceGetOutput struct {
	SchemaVersion   int             `json:"schema_version"`
	OK              bool            `json:"ok"`
	EntityID        string          `json:"entity_id"`
	Domain          string          `json:"domain"`
	UniqueID        string          `json:"unique_id,omitempty"`
	RunID           string          `json:"run_id,omitempty"`
	ItemID          string          `json:"item_id,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	LastStep        string          `json:"last_step,omitempty"`
	State           string          `json:"state,omitempty"`
	ScriptExecution string          `json:"script_execution,omitempty"`
	Error           string          `json:"error,omitempty"`
	Trace           json.RawMessage `json:"trace,omitempty"`
}

type traceListEntry struct {
	RunID           string `json:"run_id"`
	Timestamp       string `json:"timestamp,omitempty"`
	LastStep        string `json:"last_step,omitempty"`
	State           string `json:"state,omitempty"`
	ScriptExecution string `json:"script_execution,omitempty"`
	Error           string `json:"error,omitempty"`
	Trigger         string `json:"trigger,omitempty"`
}

func runTraceCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova trace <latest|list|get> <automation.entity_id|script.entity_id> [run_id] [--json]")
		return 1
	}
	switch args[0] {
	case "latest":
		return runTraceLatest(paths, args[1:])
	case "list":
		return runTraceList(paths, args[1:])
	case "get":
		return runTraceGet(paths, args[1:])
	default:
		printErr("Unknown trace command: %s", args[0])
		return 1
	}
}

func runTraceLatest(paths runtimePaths, args []string) int {
	entityID, jsonOut, err := parseTraceLatestArgs(args)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	domain, ok := traceDomainFromEntityID(entityID)
	if !ok {
		printErr("trace latest supports only automation.<id> or script.<id>")
		return 1
	}

	out, err := loadLatestTrace(paths, entityID, domain)
	if err != nil {
		out = traceLatestOutput{
			SchemaVersion: 1,
			OK:            false,
			EntityID:      entityID,
			Domain:        domain,
			Error:         err.Error(),
		}
		if jsonOut {
			printTraceLatestJSON(out)
		} else {
			printErr("%s", err)
		}
		return 1
	}
	if jsonOut {
		printTraceLatestJSON(out)
		return 0
	}
	fmt.Fprintf(os.Stdout, "entity_id: %s\n", out.EntityID)
	fmt.Fprintf(os.Stdout, "domain: %s\n", out.Domain)
	fmt.Fprintf(os.Stdout, "unique_id: %s\n", out.UniqueID)
	fmt.Fprintf(os.Stdout, "run_id: %s\n", out.RunID)
	if out.Timestamp != "" {
		fmt.Fprintf(os.Stdout, "timestamp: %s\n", out.Timestamp)
	}
	if out.LastStep != "" {
		fmt.Fprintf(os.Stdout, "last_step: %s\n", out.LastStep)
	}
	return 0
}

func runTraceList(paths runtimePaths, args []string) int {
	entityID, jsonOut, err := parseTraceEntityArgs("trace list", args)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	domain, ok := traceDomainFromEntityID(entityID)
	if !ok {
		printErr("trace list supports only automation.<id> or script.<id>")
		return 1
	}

	out, err := loadTraceList(paths, entityID, domain)
	if err != nil {
		out = traceListOutput{
			SchemaVersion: 1,
			OK:            false,
			EntityID:      entityID,
			Domain:        domain,
			Error:         err.Error(),
		}
		if jsonOut {
			printTraceListJSON(out)
		} else {
			printErr("%s", err)
		}
		return 1
	}
	if jsonOut {
		printTraceListJSON(out)
		return 0
	}
	fmt.Fprintf(os.Stdout, "entity_id: %s\n", out.EntityID)
	fmt.Fprintf(os.Stdout, "domain: %s\n", out.Domain)
	fmt.Fprintf(os.Stdout, "unique_id: %s\n", out.UniqueID)
	fmt.Fprintf(os.Stdout, "count: %d\n", out.Count)
	for _, trace := range out.Traces {
		fmt.Fprintf(os.Stdout, "- run_id: %s", trace.RunID)
		if trace.Timestamp != "" {
			fmt.Fprintf(os.Stdout, " timestamp: %s", trace.Timestamp)
		}
		if trace.LastStep != "" {
			fmt.Fprintf(os.Stdout, " last_step: %s", trace.LastStep)
		}
		if trace.Error != "" {
			fmt.Fprintf(os.Stdout, " error: %s", trace.Error)
		}
		fmt.Fprintln(os.Stdout)
	}
	return 0
}

func runTraceGet(paths runtimePaths, args []string) int {
	entityID, runID, jsonOut, err := parseTraceGetArgs(args)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	domain, ok := traceDomainFromEntityID(entityID)
	if !ok {
		printErr("trace get supports only automation.<id> or script.<id>")
		return 1
	}
	out, err := loadTraceGet(paths, entityID, domain, runID)
	if err != nil {
		out = traceGetOutput{
			SchemaVersion: 1,
			OK:            false,
			EntityID:      entityID,
			Domain:        domain,
			RunID:         runID,
			Error:         err.Error(),
		}
		if jsonOut {
			printTraceGetJSON(out)
		} else {
			printErr("%s", err)
		}
		return 1
	}
	if jsonOut {
		printTraceGetJSON(out)
		return 0
	}
	fmt.Fprintf(os.Stdout, "entity_id: %s\n", out.EntityID)
	fmt.Fprintf(os.Stdout, "domain: %s\n", out.Domain)
	fmt.Fprintf(os.Stdout, "unique_id: %s\n", out.UniqueID)
	fmt.Fprintf(os.Stdout, "run_id: %s\n", out.RunID)
	if out.Timestamp != "" {
		fmt.Fprintf(os.Stdout, "timestamp: %s\n", out.Timestamp)
	}
	if out.LastStep != "" {
		fmt.Fprintf(os.Stdout, "last_step: %s\n", out.LastStep)
	}
	if out.Error != "" {
		fmt.Fprintf(os.Stdout, "error: %s\n", out.Error)
	}
	return 0
}

func parseTraceLatestArgs(args []string) (string, bool, error) {
	return parseTraceEntityArgs("trace latest", args)
}

func parseTraceGetArgs(args []string) (string, string, bool, error) {
	fs := flag.NewFlagSet("trace get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var positional []string
	for _, arg := range args {
		if arg == "--json" {
			if err := fs.Set("json", "true"); err != nil {
				return "", "", false, err
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", "", false, fmt.Errorf("unknown trace get flag: %s", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 2 {
		return "", "", false, fmt.Errorf("Usage: ha-nova trace get <automation.entity_id|script.entity_id> <run_id> [--json]")
	}
	return strings.TrimSpace(positional[0]), strings.TrimSpace(positional[1]), *jsonOut, nil
}

func parseTraceEntityArgs(command string, args []string) (string, bool, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var positional []string
	for _, arg := range args {
		if arg == "--json" {
			if err := fs.Set("json", "true"); err != nil {
				return "", false, err
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", false, fmt.Errorf("unknown %s flag: %s", command, arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 1 {
		return "", false, fmt.Errorf("Usage: ha-nova %s <automation.entity_id|script.entity_id> [--json]", command)
	}
	return strings.TrimSpace(positional[0]), *jsonOut, nil
}

func traceDomainFromEntityID(entityID string) (string, bool) {
	switch {
	case strings.HasPrefix(entityID, "automation.") && len(entityID) > len("automation."):
		return "automation", true
	case strings.HasPrefix(entityID, "script.") && len(entityID) > len("script."):
		return "script", true
	default:
		return "", false
	}
}

func loadLatestTrace(paths runtimePaths, entityID, domain string) (traceLatestOutput, error) {
	listOut, err := loadTraceList(paths, entityID, domain)
	if err != nil {
		return traceLatestOutput{}, err
	}
	if len(listOut.Traces) == 0 {
		return traceLatestOutput{}, fmt.Errorf("no traces found for %s; Home Assistant keeps only recent traces, and YAML automations/scripts need an id to be traceable", entityID)
	}
	latest := listOut.Traces[0]
	getOut, err := loadTraceGetWithUniqueID(paths, entityID, domain, listOut.UniqueID, latest.RunID)
	if err != nil {
		return traceLatestOutput{}, err
	}
	return traceLatestOutput{
		SchemaVersion: 1,
		OK:            true,
		EntityID:      entityID,
		Domain:        domain,
		UniqueID:      listOut.UniqueID,
		RunID:         latest.RunID,
		Timestamp:     latest.Timestamp,
		LastStep:      getOut.LastStep,
		Error:         getOut.Error,
		Trace:         getOut.Trace,
	}, nil
}

func loadTraceList(paths runtimePaths, entityID, domain string) (traceListOutput, error) {
	uniqueID, err := resolveTraceUniqueID(paths, entityID)
	if err != nil {
		return traceListOutput{}, err
	}
	listPayload := map[string]string{
		"type":    "trace/list",
		"domain":  domain,
		"item_id": uniqueID,
	}
	listBody, err := relayWSJSON(paths, listPayload)
	if err != nil {
		return traceListOutput{}, err
	}
	entries, err := parseTraceList(listBody)
	if err != nil {
		return traceListOutput{}, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return traceTimestampAfter(entries[i].Timestamp, entries[j].Timestamp)
	})
	return traceListOutput{
		SchemaVersion: 1,
		OK:            true,
		EntityID:      entityID,
		Domain:        domain,
		UniqueID:      uniqueID,
		Count:         len(entries),
		Traces:        entries,
	}, nil
}

func loadTraceGet(paths runtimePaths, entityID, domain, runID string) (traceGetOutput, error) {
	uniqueID, err := resolveTraceUniqueID(paths, entityID)
	if err != nil {
		return traceGetOutput{}, err
	}
	return loadTraceGetWithUniqueID(paths, entityID, domain, uniqueID, runID)
}

func loadTraceGetWithUniqueID(paths runtimePaths, entityID, domain, uniqueID, runID string) (traceGetOutput, error) {
	if strings.TrimSpace(runID) == "" {
		return traceGetOutput{}, fmt.Errorf("trace get requires a run_id from trace list")
	}
	getPayload := map[string]string{
		"type":    "trace/get",
		"domain":  domain,
		"item_id": uniqueID,
		"run_id":  runID,
	}
	traceBody, err := relayWSJSON(paths, getPayload)
	if err != nil {
		return traceGetOutput{}, err
	}
	summary := traceGetSummary(traceBody)
	return traceGetOutput{
		SchemaVersion:   1,
		OK:              true,
		EntityID:        entityID,
		Domain:          domain,
		UniqueID:        uniqueID,
		RunID:           runID,
		ItemID:          summary.ItemID,
		Timestamp:       summary.Timestamp,
		LastStep:        summary.LastStep,
		State:           summary.State,
		ScriptExecution: summary.ScriptExecution,
		Error:           summary.Error,
		Trace:           extractRelayData(traceBody),
	}, nil
}

func resolveTraceUniqueID(paths runtimePaths, entityID string) (string, error) {
	body, err := relayWSJSON(paths, map[string]string{
		"type":      "config/entity_registry/get",
		"entity_id": entityID,
	})
	if err != nil {
		return "", err
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			UniqueID string `json:"unique_id"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("cannot parse entity registry response: %w", err)
	}
	if !envelope.OK {
		if envelope.Error.Message != "" {
			return "", fmt.Errorf("cannot resolve %s: %s", entityID, envelope.Error.Message)
		}
		return "", fmt.Errorf("cannot resolve %s", entityID)
	}
	if strings.TrimSpace(envelope.Data.UniqueID) == "" {
		return "", fmt.Errorf("cannot resolve %s: entity registry entry has no unique_id", entityID)
	}
	return envelope.Data.UniqueID, nil
}

func relayWSJSON(paths runtimePaths, payload any) ([]byte, error) {
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	token, err := readRelayAuthToken()
	if err != nil {
		return nil, fmt.Errorf("%s", relayAuthTokenProblemMessage(err))
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(cfg.RelayBaseURL, "/") + "/ws"
	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("relay ws failed with HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("cannot parse relay ws response: %w", err)
	}
	if !envelope.OK {
		if envelope.Error.Message != "" {
			return nil, fmt.Errorf("relay ws failed: %s", envelope.Error.Message)
		}
		return nil, fmt.Errorf("relay ws failed")
	}
	return body, nil
}

func parseTraceList(body []byte) ([]traceListEntry, error) {
	data := extractRelayData(body)
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("cannot parse trace list: %w", err)
	}
	return collectTraceListEntries(raw), nil
}

func collectTraceListEntries(raw any) []traceListEntry {
	var entries []traceListEntry
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			entries = append(entries, traceEntryFromAny(item))
		}
	case map[string]any:
		if nested, ok := v["traces"]; ok {
			return collectTraceListEntries(nested)
		}
		for key, item := range v {
			entry := traceEntryFromAny(item)
			if entry.RunID == "" {
				entry.RunID = key
			}
			entries = append(entries, entry)
		}
	}
	compact := entries[:0]
	for _, entry := range entries {
		if strings.TrimSpace(entry.RunID) != "" {
			compact = append(compact, entry)
		}
	}
	return compact
}

func traceEntryFromAny(raw any) traceListEntry {
	m, ok := raw.(map[string]any)
	if !ok {
		return traceListEntry{}
	}
	return traceListEntry{
		RunID:           firstString(m, "run_id", "runId", "id"),
		Timestamp:       traceTimestampFromMap(m),
		LastStep:        firstString(m, "last_step", "lastStep"),
		State:           firstString(m, "state"),
		ScriptExecution: firstString(m, "script_execution", "scriptExecution"),
		Error:           firstString(m, "error"),
		Trigger:         traceTriggerSummary(m["trigger"]),
	}
}

type traceGetSummaryFields struct {
	ItemID          string
	Timestamp       string
	LastStep        string
	State           string
	ScriptExecution string
	Error           string
}

func traceGetSummary(body []byte) traceGetSummaryFields {
	data := extractRelayData(body)
	if len(data) == 0 || string(data) == "null" {
		return traceGetSummaryFields{}
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return traceGetSummaryFields{}
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return traceGetSummaryFields{}
	}
	lastStep := firstString(m, "last_step", "lastStep")
	if lastStep == "" {
		lastStep = findStringKey(raw, "last_step")
	}
	return traceGetSummaryFields{
		ItemID:          firstString(m, "item_id", "itemId"),
		Timestamp:       traceTimestampFromMap(m),
		LastStep:        lastStep,
		State:           firstString(m, "state"),
		ScriptExecution: firstString(m, "script_execution", "scriptExecution"),
		Error:           firstString(m, "error"),
	}
}

func traceTriggerSummary(raw any) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case map[string]any:
		if platform := firstString(typed, "platform"); platform != "" {
			return platform
		}
		if event := firstString(typed, "event_type", "eventType"); event != "" {
			return event
		}
		return "object"
	default:
		return ""
	}
}

func traceTimestampFromMap(m map[string]any) string {
	if value, ok := m["timestamp"]; ok {
		switch typed := value.(type) {
		case string:
			return typed
		case map[string]any:
			if start := firstString(typed, "start"); start != "" {
				return start
			}
			if finish := firstString(typed, "finish"); finish != "" {
				return finish
			}
		}
	}
	return firstString(m, "start_time")
}

func traceTimestampAfter(a, b string) bool {
	aTime, aOK := parseTraceTimestamp(a)
	bTime, bOK := parseTraceTimestamp(b)
	switch {
	case aOK && bOK:
		return aTime.After(bTime)
	case aOK:
		return true
	case bOK:
		return false
	default:
		return a > b
	}
}

func parseTraceTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%.0f", v)
			}
		}
	}
	return ""
}

func extractRelayData(body []byte) json.RawMessage {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	return envelope.Data
}

func extractLastStep(body []byte) string {
	data := extractRelayData(body)
	if len(data) == 0 {
		return ""
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	return findStringKey(raw, "last_step")
}

func findStringKey(raw any, key string) string {
	switch v := raw.(type) {
	case map[string]any:
		if value, ok := v[key]; ok {
			if s, ok := value.(string); ok {
				return s
			}
		}
		for _, child := range v {
			if found := findStringKey(child, key); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range v {
			if found := findStringKey(child, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func printTraceLatestJSON(out traceLatestOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace result: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printTraceListJSON(out traceListOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace list: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printTraceGetJSON(out traceGetOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace detail: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}
