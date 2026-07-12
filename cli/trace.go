package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Println("Usage: ha-nova trace <latest|list|get> <automation.entity_id|script.entity_id> [run_id] [--json]")
		fmt.Println("Run 'ha-nova trace <subcommand> --help' to see that subcommand's flags.")
		return 0
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
		if errors.Is(err, errHelpShown) {
			return 0
		}
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
		if errors.Is(err, errHelpShown) {
			return 0
		}
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
		if errors.Is(err, errHelpShown) {
			return 0
		}
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
