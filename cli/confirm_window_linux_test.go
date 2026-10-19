package main

import "testing"

func TestZenityAnswer(t *testing.T) {
	for _, c := range []struct {
		code   int
		stderr string
		want   windowAnswer
	}{
		{0, "", windowConfirmed},
		{1, "", windowCancelled},
		{5, "", windowNoAnswer}, // its own timeout
		{-1, "", windowClosed},  // killed from outside (lane D9)
		{1, "cannot open display", windowFailed},
	} {
		if got := zenityAnswer(c.code, c.stderr); got != c.want {
			t.Errorf("zenityAnswer(%d, %q) = %v, want %v", c.code, c.stderr, got, c.want)
		}
	}
}
