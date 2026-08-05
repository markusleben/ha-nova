package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runSnapshotCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova snapshot <save|show|verify> ...")
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Println("Usage: ha-nova snapshot <save|show|verify> ...")
		fmt.Println("Run 'ha-nova snapshot <subcommand> --help' to see that subcommand's flags.")
		return 0
	}
	switch args[0] {
	case "save":
		return runSnapshotSave(paths, args[1:])
	case "show":
		return runSnapshotShow(paths, args[1:])
	case "verify":
		return runSnapshotVerify(paths, args[1:])
	default:
		printErr("Unknown snapshot command: %s", args[0])
		return 1
	}
}

func runSnapshotSave(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot save", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dataFile string
	var dataFileSet bool
	fs.StringVar(&dataFile, "data-file", "", "path to the JSON record (defaults to stdin)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot save [--data-file <file>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "data-file" {
			dataFileSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot save takes no positional arguments; use --data-file <file> or pipe the record on stdin")
		return 1
	}
	var data []byte
	var err error
	if dataFileSet {
		if strings.TrimSpace(dataFile) == "" {
			printErr("--data-file requires a non-empty path; no snapshot was written")
			return 1
		}
		// File-based input is shell-agnostic — the canonical relay contract and
		// `ha-nova diff --before/--after` are file-based too, so the skill never
		// has to build a stdin redirect/pipe (fragile on Windows PowerShell).
		data, err = os.ReadFile(filepath.Clean(dataFile))
		if err != nil {
			printErr("cannot read snapshot record: %s", err)
			return 1
		}
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			printErr("cannot read snapshot from stdin: %s", err)
			return 1
		}
		if strings.TrimSpace(string(data)) == "" {
			printErr("snapshot save requires --data-file <record-file> or JSON on stdin")
			return 1
		}
	}
	receipt, err := saveUndoSnapshotBytes(paths, data)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	// Structured receipt on stdout (#483): the skill repeats it in the result
	// so a silent save — or a silent same-target replacement — cannot happen.
	out, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		printErr("cannot render snapshot receipt: %s", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}

func runSnapshotShow(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var target, domain string
	var list bool
	fs.StringVar(&target, "target", "", "select the snapshot for this target_id (default: newest)")
	fs.StringVar(&domain, "domain", "", "optional domain filter when selecting by target")
	fs.BoolVar(&list, "list", false, "list the stored snapshots (op/domain/target_id, newest first)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot show [--list] [--target <target_id>] [--domain <domain>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	var targetSet, domainSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "target":
			targetSet = true
		case "domain":
			domainSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot show does not accept positional arguments")
		return 1
	}
	if targetSet && strings.TrimSpace(target) == "" {
		printErr("--target requires a non-empty target_id")
		return 1
	}
	if domainSet && strings.TrimSpace(domain) == "" {
		printErr("--domain requires a non-empty domain")
		return 1
	}
	if domainSet && !targetSet {
		printErr("--domain requires --target")
		return 1
	}
	if list && (targetSet || domainSet) {
		printErr("--list cannot be combined with --target or --domain")
		return 1
	}
	if list {
		stack, err := loadUndoSnapshotStack(paths)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		entries := make([]snapshotListingEntry, 0, len(stack.Snapshots))
		for _, s := range stack.Snapshots {
			entries = append(entries, snapshotListing(s))
		}
		out, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			printErr("cannot render snapshot list: %s", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, string(out))
		return 0
	}
	snap, err := selectUndoSnapshot(paths, target, domain)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		printErr("cannot render snapshot: %s", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(out))
	return 0
}

func runSnapshotVerify(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("snapshot verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var against, target, domain string
	fs.StringVar(&against, "against", "", "path to the current live config JSON")
	fs.StringVar(&target, "target", "", "select the snapshot for this target_id (default: newest)")
	fs.StringVar(&domain, "domain", "", "optional domain filter when selecting by target")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova snapshot verify --against <live.json> [--target <target_id>] [--domain <domain>]") {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	var targetSet, domainSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "target":
			targetSet = true
		case "domain":
			domainSet = true
		}
	})
	if fs.NArg() != 0 {
		printErr("snapshot verify does not accept positional arguments")
		return 1
	}
	if targetSet && strings.TrimSpace(target) == "" {
		printErr("--target requires a non-empty target_id")
		return 1
	}
	if domainSet && strings.TrimSpace(domain) == "" {
		printErr("--domain requires a non-empty domain")
		return 1
	}
	if domainSet && !targetSet {
		printErr("--domain requires --target")
		return 1
	}
	if strings.TrimSpace(against) == "" {
		printErr("--against <live.json> is required")
		return 1
	}
	snap, err := selectUndoSnapshot(paths, target, domain)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	liveRaw, err := os.ReadFile(against)
	if err != nil {
		printErr("cannot read live config: %s", err)
		return 1
	}
	match, err := snapshotMatchesLive(snap, liveRaw)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if match {
		fmt.Fprintln(os.Stdout, "match")
		return 0
	}
	fmt.Fprintln(os.Stdout, "drift")
	return snapshotExitDrift
}

// saveUndoSnapshotBytes parses, validates and atomically writes the snapshot
// onto the bounded stack: newest first, one entry per domain+target (same
// target replaces its entry), oldest evicted beyond the limit. The store
// assumes single-flight writes: the skill's single write path (apply-agent)
// serialises operations. The write itself is atomic (temp file + rename), so a
// reader never sees a torn file even if that assumption is violated.
