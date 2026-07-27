package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCloudStatusJSONStaysJSONWhenPositionalPrecedesFlag(t *testing.T) {
	resetServerProfileSelection(t)
	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			runtimePaths{},
			[]string{"unexpected", "--json"},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("invalid-argument status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "error" ||
		summary.VerificationError == nil {
		t.Fatalf("invalid-argument JSON exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudStatusJSONRequestedRecognizesValidTrueFormsAnywhere(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "first", args: []string{"--json", "unexpected"}, want: true},
		{name: "last", args: []string{"unexpected", "--json"}, want: true},
		{name: "assignment", args: []string{"--json=true"}, want: true},
		{name: "assignment uppercase", args: []string{"bad", "--json=TRUE"}, want: true},
		{name: "assignment one", args: []string{"--json=1"}, want: true},
		{name: "assignment false", args: []string{"--json=false"}, want: false},
		{name: "assignment invalid", args: []string{"--json=yes"}, want: false},
		{name: "substring", args: []string{"prefix--json"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := scanCloudStatusArgs(test.args).jsonRequested; got != test.want {
				t.Fatalf("scanCloudStatusArgs(%q).jsonRequested = %v", test.args, got)
			}
		})
	}
}

func TestCloudStatusRawServerAfterParseErrorNeverDefaultsAnotherProfile(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(
		t,
		strings.Replace(
			testV2TwoProfileConfig,
			`"default_server": "default"`,
			`"default_server": "cabin"`,
			1,
		),
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{"unexpected", "--server", "remote", "--json=true"},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("invalid-argument status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.Server != "remote" {
		t.Fatalf("raw server attribution exit=%d summary=%+v", exit, summary)
	}
	if requested, source := requestedServerSelection(); requested != "" || source != "" {
		t.Fatalf("raw scan mutated selection: %q source=%q", requested, source)
	}

	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{"unexpected", "--server=bad name", "--json"},
		)
	})
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("invalid-selection status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.Server != "bad name" {
		t.Fatalf("invalid raw server defaulted elsewhere: %+v", summary)
	}
	if requested, source := requestedServerSelection(); requested != "" || source != "" {
		t.Fatalf("invalid raw scan mutated selection: %q source=%q", requested, source)
	}
}
