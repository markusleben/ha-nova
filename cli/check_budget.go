package main

import (
	"context"
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

// checkDeadline: when the running call must answer (main sets it for the
// command, runLocked for each mcp or serve call); zero keeps the session
// budget (tests).
var checkDeadline time.Time

// commandStart: when the running call began (main for the command, runLocked
// for each mcp or serve call). A plan's ask ends windowWait after it, so a
// slow re-plan before the ask shortens the ask instead of pushing the answer
// past the host's limit (#1026); zero keeps windowWait (tests).
var commandStart time.Time

// askWait: what is left of windowWait for this call's ask.
func askWait() time.Duration {
	if commandStart.IsZero() {
		return windowWait
	}
	return windowWait - time.Since(commandStart)
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
// reads as it did before #1009). A replay keeps its own budget
// (replayReads). Earlier contexts stay alive until close, as in renewBudget.
func (s *relaySession) readUntilDeadline() {
	if checkDeadline.IsZero() {
		return
	}
	until := later(checkDeadline.Add(-afterCheck), time.Now().Add(relaySessionOverallTimeout))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replay != nil {
		return
	}
	if current, set := s.ctx.Deadline(); set && !until.After(current) {
		return
	}
	previous := s.cancel
	ctx, cancel := context.WithDeadline(context.Background(), until)
	s.ctx, s.cancel = ctx, func() { cancel(); previous() }
}
