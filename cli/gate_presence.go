package main

import (
	"context"
	"regexp"
	"strconv"
	"time"
)

// Whether the person is at this computer while its window waits (0.32 spec
// §4, §6). The App asks the phone at once when they're away, after 20
// seconds when HA NOVA can't tell (Linux), and after 60 seconds while they
// use the computer. It only changes when the phone asks, never what is
// approved: a lying client can only delay its own owner's phone.

var (
	gateAwayAfter = 20 * time.Second
	gateIdleEvery = 5 * time.Second // how often the window's wait reads it; tests shorten it
	idleTime      = nativeIdleTime  // since the last keyboard or mouse input; ok false: no reading here
)

// gatePresence: what an ask's open says, "" when it says nothing: an App
// without the window contract, or no window with this plan's Confirm opens
// here (a chat app, windows off, no display, a held write, a plan too long
// for the window). Without presence the App asks the phone at once.
func (s *relaySession) gatePresence(plan *gateConfirm) string {
	if !s.gateWindowApp() || plan == nil || plan.text == "" || !plan.fits() || !s.windowHere() {
		return ""
	}
	idle, ok := idleTime()
	switch {
	case !ok:
		return "unknown"
	case idle >= gateAwayAfter:
		return "away"
	}
	return "present"
}

func (s *relaySession) gateWindowApp() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gate != nil && s.gate.window
}

// gateAway closes once the person has left (no input for gateAwayAfter),
// read every gateIdleEvery until ctx ends; nil where nothing can be read.
func gateAway(ctx context.Context) <-chan struct{} {
	read, every, after := idleTime, gateIdleEvery, gateAwayAfter
	if _, ok := read(); !ok {
		return nil
	}
	away := make(chan struct{})
	go func() {
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if idle, ok := read(); ok && idle >= after {
					close(away)
					return
				}
			}
		}
	}()
	return away
}

var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// parseHIDIdle reads macOS's idle time (ioreg's HIDIdleTime, nanoseconds).
func parseHIDIdle(out string) (time.Duration, bool) {
	m := hidIdle.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(m[1], 10, 64)
	return time.Duration(ns), err == nil
}

// gateGone tells the App (op away) that the person left while the window
// waits, or that it can't confirm after all: the phone asks now. Best
// effort, as gateCancel.
func (s *relaySession) gateGone(askID string) {
	var state struct{}
	_ = s.gateCall(context.Background(), gateAskRoute, 5*time.Second, map[string]any{"ask_id": askID, "op": "away"}, &state)
}
