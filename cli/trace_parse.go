package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
