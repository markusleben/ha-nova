package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every reason code the engine can emit is pinned to the prose it encodes.
func TestTierReasonCodesAreDocumented(t *testing.T) {
	source, err := os.ReadFile("tier_rules.go")
	if err != nil {
		t.Fatal(err)
	}
	var docs string
	for _, path := range []string{"../skills/ha-nova/indirect-actuation.md", "../docs/work/2026-10-14-typed-verbs-spec.md"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		docs += string(raw)
	}
	codes := regexp.MustCompile(`(?m)^\s+(?:reason|note)\w+\s+= "([A-Z_]+)"$`).FindAllStringSubmatch(string(source), -1)
	if len(codes) < 30 {
		t.Fatalf("found only %d reason codes; did the const block move?", len(codes))
	}
	for _, code := range codes {
		if !strings.Contains(docs, "`"+code[1]+"`") {
			t.Errorf("reason code %s is not documented in indirect-actuation.md or the typed-verbs spec", code[1])
		}
	}
}
