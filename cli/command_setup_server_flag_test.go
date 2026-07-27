package main

import (
	"strings"
	"testing"
)

func TestSetupServerFlagRejectsEmptyOrWhitespaceName(t *testing.T) {
	for _, value := range []string{"", " cabin"} {
		t.Run(value, func(t *testing.T) {
			resetServerProfileSelection(t)
			exit, output := captureCommandOutput(t, func() int {
				return runSetup(
					runtimePaths{},
					[]string{"--server", value},
				)
			})
			if exit != 1 ||
				!strings.Contains(output, "invalid server profile name") {
				t.Fatalf(
					"setup --server %q exit=%d output=%q",
					value,
					exit,
					output,
				)
			}
		})
	}
}
