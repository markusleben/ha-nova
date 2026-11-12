package main

import (
	"context"
	"slices"
	"time"
)

// The check deadline (#1009): an impact check reads until shortly before
// the call that runs it must answer, not until a fixed 30 s; afterCheck is
// left for what a typed plan does next (the phone lookup). The session budget
// (relaySessionOverallTimeout) still bounds every other phase. A check the
// deadline cuts stays typed (LIMIT_CUT, "read budget") and says it ran out
// of time.
const (
	cliCheckBudget = 90 * time.Second // a CLI command: Claude Code's 120 s command limit
	mcpCheckBudget = 45 * time.Second // an MCP tool call: the common 60 s limit
	chatCallBudget = 55 * time.Second // a chat app gives up about a minute after its request
	afterCheck     = 10 * time.Second
)

// checkDeadline: when the CLI command must answer (main sets it); an mcp or
// serve call has its own (verbCall.deadline). Zero keeps the session budget
// (tests).
var checkDeadline time.Time

// commandStart: when the CLI command began (main); an mcp or serve call has
// its own (verbCall.start). A plan's ask ends windowWait after it, so a
// slow re-plan before the ask shortens the ask instead of pushing the answer
// past the host's limit (#1026); zero keeps windowWait (tests).
var commandStart time.Time

// askWait: what is left of windowWait for this call's ask.
func (s *relaySession) askWait() time.Duration {
	start := s.call.began()
	if start.IsZero() {
		return windowWait
	}
	return windowWait - time.Since(start)
}

// chatCheckDeadline: a chat-app request answers about a minute after it
// came in (since); after a long wait for the lock, the floor below wins.
func chatCheckDeadline(since time.Time) time.Time { return since.Add(chatCallBudget) }

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// readUntilDeadline extends the session's read budget for one check to
// afterCheck before checkDeadline, and never to less than a session budget
// from now (a check after a long wait, such as apply's after the window,
// reads as it did before #1009). Before a person's ask, a cut plan's check
// ends at askCeiling instead, even sooner than that floor: its replay
// decides. A replay keeps its own budget (replayReads). Earlier contexts
// stay alive until close, as in renewBudget.
func (s *relaySession) readUntilDeadline() {
	if s == nil { // an engine without a session reads nothing (tests)
		return
	}
	deadline := s.call.answerBy()
	if deadline.IsZero() {
		return
	}
	until := later(deadline.Add(-afterCheck), time.Now().Add(relaySessionOverallTimeout))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replay != nil {
		return
	}
	capped := !s.askCeiling.IsZero() && until.After(s.askCeiling)
	if capped {
		until = s.askCeiling
	}
	if current, set := s.ctx.Deadline(); set && !until.After(current) && !capped {
		return
	}
	previous := s.cancel
	ctx, cancel := context.WithDeadline(context.Background(), until)
	s.ctx, s.cancel = ctx, func() { cancel(); previous() }
}

// recheckBeforeAsk is recheck before a person's ask. A cut plan's check
// (or a batch's cut member's) then ends at askCeiling: its replay, the gate
// read or phone lookup (afterCheck), opening the ask (one gate worker
// step), and windowMinWait for the person still fit in the call's ask
// (askWait); the CLI's check gets 30 s, as the cut-plan spec planned. Read to checkDeadline, a big home on a slow link left the
// ask no time (#827 on a gated App). An uncut plan checks as before, and so
// does a plan whose ask has a life of its own (approval: an MCP or chat
// job) or whose call leaves no time for a capped check (MCP's 40 s window):
// there the ask refuses once its time is gone.
func recheckBeforeAsk(s *relaySession, env *verbEnvelope, stored storedPlan, compute func(*verbEnvelope) (storedPlan, error)) (storedPlan, error) {
	start := s.call.began()
	ceiling := start.Add(windowWait - windowMinWait - replayBudget - afterCheck - gateWorkerStep)
	cut := func(p storedPlan) bool { return len(p.Reads) > 0 && slices.ContainsFunc(p.Reasons, cutShort) }
	if s.approval() == nil && !start.IsZero() && ceiling.After(time.Now()) && (cut(stored) || slices.ContainsFunc(stored.BatchMembers, cut)) {
		s.mu.Lock()
		s.askCeiling = ceiling
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.askCeiling = time.Time{}
			s.mu.Unlock()
		}()
	}
	return recheck(s, env, stored, compute)
}
