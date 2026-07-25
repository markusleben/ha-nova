package main

import (
	"os"
	"strings"
	"testing"
)

func TestServerRemoveOnlyCloudProfilePointsAtFullPurge(t *testing.T) {
	paths := setupServerCommandTest(t, `{
		"schema_version":3,
		"default_server":"cabin",
		"servers":{
			"cabin":{
				"profile_id":"profile-cabin",
				"cloud":{"state":"authorizing"}
			}
		}
	}`)
	stubServerCommandStdin(t, "cabin\n")

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "ha-nova uninstall --purge") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
}

func TestServerRemoveRejectsNullProfileBeforeMutation(t *testing.T) {
	paths := setupServerCommandTest(t, `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{"relay_base_url":"http://ha:8791"},
			"cabin":null
		}
	}`)

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || !strings.Contains(output, "nothing was removed") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
}

func TestServerRemoveRefusesToStrandCloudCredentials(t *testing.T) {
	paths := setupServerCommandTest(t, routeCommandConfig)
	stubServerCommandStdin(t, "cabin\n")
	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "native secure-storage credentials") ||
		!strings.Contains(output, "Nothing was removed") {
		t.Fatalf("exit=%d output=%s", exit, output)
	}
}

func TestServerRemoveFailsClosedForUnknownOrMalformedCloudProfile(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
	}{
		{
			name: "unknown Cloud shape",
			config: `{"schema_version":3,"default_server":"default","servers":{` +
				`"default":{"relay_base_url":"http://ha:8791"},` +
				`"cabin":{"relay_base_url":"http://cabin:8791","cloud":{"future_state":"opaque"}}}}`,
		},
		{
			name: "malformed profile",
			config: `{"schema_version":3,"default_server":"default","servers":{` +
				`"default":{"relay_base_url":"http://ha:8791"},"cabin":"corrupt"}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := setupServerCommandTest(t, test.config)
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			exit, output := captureCommandOutput(t, func() int {
				return runServerCommand(
					paths,
					[]string{"remove", "cabin"},
				)
			})
			if exit != 1 ||
				!strings.Contains(
					strings.ToLower(output),
					"nothing was removed",
				) {
				t.Fatalf("exit=%d output=%s", exit, output)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("fail-closed Cloud inspection changed config")
			}
		})
	}
}
