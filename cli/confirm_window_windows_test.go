package main

import "testing"

// The box's result, as answered and as found when we withdrew it: only a
// Cancel the person gave before the withdrawal counts then; our own
// WM_CLOSE is never read (nativeWindow stops reading once it posts one).
func TestWindowsAnswers(t *testing.T) {
	for _, c := range []struct {
		r                uintptr
		answer, withdrew windowAnswer
	}{
		{0, windowFailed, windowNoAnswer},
		{1, windowConfirmed, windowNoAnswer},
		{2, windowCancelled, windowCancelled},
		{32000, windowNoAnswer, windowNoAnswer}, // MB_TIMEDOUT
	} {
		if got := windowsAnswer(c.r); got != c.answer {
			t.Errorf("windowsAnswer(%d) = %v, want %v", c.r, got, c.answer)
		}
		if got := windowsWithdrawn(c.r); got != c.withdrew {
			t.Errorf("windowsWithdrawn(%d) = %v, want %v", c.r, got, c.withdrew)
		}
	}
}
