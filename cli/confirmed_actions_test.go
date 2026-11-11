package main

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite generated files")

const confirmedActionsDocPath = "../skills/ha-nova/confirmed-actions.md"

// The generated list stays current: go test -run TestConfirmedActionsDoc -update
// rewrites it.
func TestConfirmedActionsDoc(t *testing.T) {
	want := renderConfirmedActionsDoc()
	if *updateGolden {
		if err := os.WriteFile(confirmedActionsDocPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(confirmedActionsDocPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale: run go test -run TestConfirmedActionsDoc -update ./cli", confirmedActionsDocPath)
	}
}

// Every reason code the engine defines is either listed as one a person
// confirms or classified on purpose as one that is not; every raw why is
// listed; the rows agree with the typed-verbs spec table.
func TestConfirmedActionsCoverEngine(t *testing.T) {
	sources, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constants := regexp.MustCompile(`(?m)^\s*(?:const )?(?:reason|note)\w+\s+= "([A-Z][A-Z0-9_]*)"`)
	literalCode := regexp.MustCompile(`tierReason\{Code:\s*"|tier(?:Typed|Preview|Refused),\s*"[A-Z]|\.gap\("[A-Z]`)
	engine := map[string]bool{}
	for _, entry := range sources {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range constants.FindAllStringSubmatch(string(raw), -1) {
			engine[code[1]] = true
		}
		if literalCode.Match(raw) {
			t.Errorf("%s names a reason code by literal; define a constant so confirmed_actions.go has to list it", name)
		}
	}
	if len(engine) < 50 {
		t.Fatalf("found only %d reason codes; did the constants move?", len(engine))
	}

	listed, rawListed := map[string]bool{}, map[string]bool{}
	for _, group := range confirmedActions {
		if group.Title == "" || group.Summary == "" || len(group.Actions) == 0 {
			t.Errorf("group %q needs a title, a summary, and rows", group.Title)
		}
		for _, action := range group.Actions {
			if action.Example == "" || strings.HasPrefix(action.Example, " ") || strings.HasSuffix(action.Example, ".") {
				t.Errorf("%s: example %q should be one short line without a final period", action.Code, action.Example)
			}
			id, raw := strings.CutPrefix(action.Code, confirmedRawPrefix)
			if listed[action.Code] {
				t.Errorf("%s is listed twice", action.Code)
			}
			listed[action.Code] = true
			switch {
			case raw:
				rawListed[id] = true
				if rawWhys[id] == nil {
					t.Errorf("%s: %q is not a key of rawWhys", action.Code, id)
				}
			case !engine[action.Code]:
				t.Errorf("%s is listed but the engine has no such reason code", action.Code)
			case confirmedNotTyped[action.Code]:
				t.Errorf("%s is both listed as confirmed and classified as not typed", action.Code)
			}
		}
	}
	for code := range engine {
		if code != reasonRawTyped && !listed[code] && !confirmedNotTyped[code] {
			t.Errorf("reason code %s is in neither confirmedActions nor confirmedNotTyped (confirmed_actions.go)", code)
		}
	}
	for code := range confirmedNotTyped {
		if !engine[code] {
			t.Errorf("confirmedNotTyped lists %s, which the engine does not define", code)
		}
	}
	for id := range rawWhys {
		if !rawListed[id] {
			t.Errorf("raw why %q is not in confirmedActions (confirmed_actions.go)", id)
		}
	}

	spec, err := os.ReadFile("../docs/work/2026-10-14-typed-verbs-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `([A-Z_]+)`( \\([a-z]+\\))? \\| (typed|preview|refused|note) \\|").FindAllStringSubmatch(string(spec), -1)
	if len(rows) < 30 {
		t.Fatalf("found only %d spec rows", len(rows))
	}
	for _, row := range rows {
		if row[2] != "" {
			continue // a variant of the code's main row (DISRUPTIVE button)
		}
		if typed := listed[row[1]]; engine[row[1]] && (row[3] == "typed" && !typed || (row[3] == "preview" || row[3] == "note") && typed) {
			t.Errorf("%s: the spec rates it %s, confirmed_actions.go lists it %v", row[1], row[3], typed)
		}
	}
}

// The MCP instructions carry the table's summary, not a hand-written copy.
func TestConfirmedActionsInMCPInstructions(t *testing.T) {
	if !strings.Contains(mcpInstructions, "(four digits; "+confirmedActionsSummary()+") needs the\nuser") {
		t.Fatalf("instructions must carry the confirmed-actions summary: %s", mcpInstructions)
	}
	if strings.Contains(mcpInstructions, "garage doors, alarm disarm, deletes)") {
		t.Fatal("the hand-written list is back in the instructions")
	}
}
