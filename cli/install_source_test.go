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

func TestIsWingetUpdateNotApplicableExitCodeHandlesSignedAndUnsignedRepresentations(t *testing.T) {
	if !isWingetUpdateNotApplicableExitCode(int(wingetUpdateNotApplicableExitCode)) {
		t.Fatal("expected unsigned winget update-not-applicable exit code to be recognized")
	}
	if !isWingetUpdateNotApplicableExitCode(-1978335189) {
		t.Fatal("expected signed winget update-not-applicable exit code to be recognized")
	}
	if isWingetUpdateNotApplicableExitCode(1) {
		t.Fatal("did not expect generic exit code to be treated as update-not-applicable")
	}
}

func TestHelperProcessRunWingetUninstall(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS_RUN_WINGET_UNINSTALL") != "1" {
		return
	}
	os.Exit(0)
}
