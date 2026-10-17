package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDarwinWindowAnswers(t *testing.T) {
	exited := func(code string) error { return exec.Command("/bin/sh", "-c", "exit "+code).Run() }
	for _, c := range []struct {
		out     string
		err     error
		ended   bool
		showing bool
		want    windowAnswer
	}{
		{"button Confirm", nil, false, true, windowConfirmed},
		{"cancel", nil, false, true, windowCancelled},
		{"timeout", nil, false, true, windowNoAnswer},
		{"error -1712", nil, false, true, windowNoAnswer},
		{"error -1713", nil, false, true, windowFailed}, // no user interaction: it never showed
		{"error -50", nil, false, true, windowNoAnswer}, // any other error once the script reached the window
		{"", exited("1"), false, false, windowFailed},   // did not compile (sandbox, -2740)
		{"", exited("1"), false, true, windowNoAnswer},  // interrupted after reaching the window
		{"", errors.New("exec: no such file"), false, false, windowFailed},
		{"button Confirm", nil, true, true, windowNoAnswer},                                   // our own deadline ended it
		{"", exec.Command("/bin/sh", "-c", "kill -9 $$").Run(), false, false, windowNoAnswer}, // killed
		{"button Cancel", nil, false, true, windowNoAnswer},
		{"button Confirm extra", nil, false, true, windowNoAnswer},
	} {
		if got := darwinAnswer(c.out, c.err, c.ended, c.showing); got != c.want {
			t.Errorf("darwinAnswer(%q, %v, %v, %v) = %v, want %v", c.out, c.err, c.ended, c.showing, got, c.want)
		}
	}
}

// TestRealWindow opens the real window for two seconds and lets it time out
// (HA_NOVA_REAL_WINDOW=timeout), or expects it to fail inside a sandbox
// (HA_NOVA_REAL_WINDOW=failed). Manual check only; never in CI.
func TestRealWindow(t *testing.T) {
	want := map[string]windowAnswer{"timeout": windowNoAnswer, "failed": windowFailed}
	expected, set := want[os.Getenv("HA_NOVA_REAL_WINDOW")]
	if !set {
		t.Skip("manual: HA_NOVA_REAL_WINDOW=timeout|failed")
	}
	t.Logf("available: %v", nativeWindowAvailable())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if got := nativeWindow(ctx, "HA NOVA test — nothing happens. Unlock “Front Door”?\n\n"+windowAsked, 2*time.Second); got != expected {
		t.Fatalf("real window: %v, want %v", got, expected)
	}
}
