package main

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestZenityAnswer(t *testing.T) {
	for _, c := range []struct {
		code   int
		stderr string
		ended  bool
		want   windowAnswer
	}{
		{0, "", false, windowConfirmed},
		{1, "", false, windowCancelled},
		{5, "", false, windowNoAnswer}, // its own timeout
		{-1, "", false, windowClosed},  // killed from outside (lane D9)
		{1, "cannot open display", false, windowFailed},
		{1, "", true, windowCancelled}, // a Cancel given just before we withdrew it still counts
		{-1, "", true, windowNoAnswer}, // our own withdrawal
		{0, "", true, windowNoAnswer},  // a Confirm after the withdrawal began never counts
	} {
		if got := zenityAnswer(c.code, c.stderr, c.ended); got != c.want {
			t.Errorf("zenityAnswer(%d, %q, %v) = %v, want %v", c.code, c.stderr, c.ended, got, c.want)
		}
	}
}

func init() { windowHelperAttr = dialogAttr }

// SIGKILL can't be caught: the window still dies with the CLI (Pdeathsig).
func TestWindowDiesWithAKilledCLI(t *testing.T) {
	cli, window, _ := startWindowHelper(t, "pdeathsig")
	if err := cli.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cli.Wait(); !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("the CLI was not killed: %v", err)
	}
	if !windowGone(window, 5*time.Second) {
		t.Fatal("the window outlived the killed CLI")
	}
}
