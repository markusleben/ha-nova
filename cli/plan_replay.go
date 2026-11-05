package main

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Replay of a cut preview (docs/work/2026-11-03-cut-plan-drift-spec.md). A
// check cut short stopped where its read budget ran out, so a recompute
// that reads on stops elsewhere and differs in what it read, not in the
// home. A cut plan keeps the reads its preview made (storedPlan.Reads); its
// replay reads exactly those again and nothing else, so it sees what the
// preview saw, as it is now. A read the preview never made means the home
// changed where the preview looked: the replay is drift (diverged).

// errNotReplayed: a read the preview had no answer to; the replay fails it
// as the preview did.
var errNotReplayed = errors.New("the preview had no answer to this read")

// replayBudget: the replay's own read budget.
const replayBudget = 15 * time.Second

// reads: every read the session made, true where Home Assistant answered
// (a refusal is an answer too), and the walk's choices that hung on the
// clock (decide).
func (s *relaySession) reads() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for key, m := range s.memo {
		select {
		case <-m.done:
			out[key] = answered(m.err)
		default: // in flight: no answer
			out[key] = false
		}
	}
	return out
}

// decide records a choice the walk made from the clock (now) under key, or
// in a replay gives the preview's choice.
func (s *relaySession) decide(key string, now bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replay != nil {
		made, known := s.replay[key]
		s.diverged = s.diverged || !known
		return made
	}
	if m, made := s.memo[key]; made { // the first choice stands, as in its replay
		return m.err == nil
	}
	m := &relayMemo{done: make(chan struct{})}
	if !now {
		m.err = errNotReplayed
	}
	close(m.done)
	s.memo[key] = m
	return now
}

// answered: Home Assistant answered, with data or its own refusal of the
// command (relay_write.go: rejectedByHA); no answer in time or over the
// Relay's link is none.
func answered(err error) bool {
	var call *relayCallError
	return err == nil || errors.As(err, &call) && call.code == "UPSTREAM_WS_COMMAND_ERROR"
}

// lost: in a replay, a read the preview had an answer to got none now (the
// budget, a timeout): the replay can't tell whether it changed. The
// listener scan's own config reads stay a gap, as in any recompute: they
// never enter the basis (tierEngine.census).
func (s *relaySession) lost(replaying bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if replaying && !answered(err) && !s.census {
		s.diverged = true
	}
}

// censusReads marks the listener scan's config reads running (lost).
func (s *relaySession) censusReads(running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.census = running
}

func (s *relaySession) replaying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replay != nil
}

// replayReads runs compute on the reads a cut preview made: the answered
// ones are read again first (in parallel, on a budget of their own), the
// others fail as they did, and any other read, or an answered one that got
// no answer now, marks the result diverged. A replay that did not diverge
// keeps those reads, so a plan saved from it replays the same way.
func (s *relaySession) replayReads(made map[string]bool, compute func() (storedPlan, error)) (storedPlan, error) {
	s.forgetFailed()
	defer s.ownBudget(time.Now().Add(replayBudget))()
	var reads []func()
	for key, answered := range made {
		endpoint, body, _ := strings.Cut(key, " ")
		var payload map[string]any
		if answered && json.Unmarshal([]byte(body), &payload) == nil {
			reads = append(reads, func() { _, _ = s.read(endpoint, payload) }) // the read allowlist still applies
		}
	}
	prefetch(reads)
	s.mu.Lock()
	s.replay, s.diverged = made, false
	s.mu.Unlock()
	fresh, err := compute()
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh.replayed, fresh.diverged, s.replay = true, s.diverged, nil
	if !fresh.diverged {
		fresh.Reads = made
	}
	return fresh, err
}
