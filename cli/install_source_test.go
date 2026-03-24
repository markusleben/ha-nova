package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunWingetUninstallUsesPurgeFlags(t *testing.T) {
	originalLookPath := execLookPathForLifecycle
	originalExec := execCommandForLifecycle
	defer func() {
		execLookPathForLifecycle = originalLookPath
		execCommandForLifecycle = originalExec
	}()

	execLookPathForLifecycle = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}

	var gotName string
	var gotArgs []string
	execCommandForLifecycle = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, args...)
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessRunWingetUninstall")
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS_RUN_WINGET_UNINSTALL=1")
		return cmd
	}

	if err := runWingetUninstall(uninstallModePurge); err != nil {
		t.Fatalf("runWingetUninstall() error: %v", err)
	}
	if gotName != "winget" {
		t.Fatalf("command = %q, want winget", gotName)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"uninstall", "--id", wingetPackageID, "--exact", "--accept-source-agreements", "--purge", "--force"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("winget uninstall args missing %q: %q", want, joined)
		}
	}
}

func TestHelperProcessRunWingetUninstall(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS_RUN_WINGET_UNINSTALL") != "1" {
		return
	}
	os.Exit(0)
}
