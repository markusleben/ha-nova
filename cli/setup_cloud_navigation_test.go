package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestCloudOnlyURLBackReturnsToConnectionMode(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)

	stdout, stderr := captureInteractiveSetupIO(
		t,
		"3\nback\nexit\n",
		func() int {
			return interactiveSetup(
				paths,
				runtimeConfig{},
				installState{},
				"claude",
				"",
				"",
				"",
				"",
				false,
			)
		},
	)
	output := stdout + stderr
	if strings.Count(
		output,
		"How should this computer connect to Home Assistant?",
	) != 2 {
		t.Fatalf("Cloud URL back did not return to connection mode:\n%s", output)
	}
	if strings.Count(output, "Setup cancelled") != 1 ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"Cloud URL back changed state or cancelled early; calls=%d:\n%s",
			coordinator.remoteCalls,
			output,
		)
	}
}

func TestConnectionModeBackReturnsToClientSelection(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	installCloudCommandCoordinator(t, newSelectingCloudCoordinator())

	stdout, stderr := captureInteractiveSetupIO(
		t,
		"back\n1\nexit\n",
		func() int {
			return interactiveSetup(
				paths,
				runtimeConfig{},
				installState{},
				"claude",
				"",
				"",
				"",
				"",
				false,
			)
		},
	)
	output := stdout + stderr
	if strings.Count(
		output,
		"How should this computer connect to Home Assistant?",
	) != 2 ||
		!strings.Contains(output, "Which AI client do you use?") {
		t.Fatalf("connection-mode back did not return to client selection:\n%s", output)
	}
	if strings.Count(output, "Setup cancelled") != 1 {
		t.Fatalf("back was treated as process cancellation:\n%s", output)
	}
}

func TestCloudOnlyURLExitStillCancels(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)

	exit, output := captureCommandOutput(t, func() int {
		return runInteractiveCloudOnlySetup(
			bufio.NewReader(strings.NewReader("exit\n")),
			os.Stdout,
			paths,
			runtimeConfig{},
			&installState{},
			"claude",
			[]string{"claude"},
			nil,
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "Setup cancelled") ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"URL exit did not preserve cancellation; exit=%d calls=%d:\n%s",
			exit,
			coordinator.remoteCalls,
			output,
		)
	}
}
