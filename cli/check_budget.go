package main

import (
	"context"
	"time"
)

// The check deadline (#1009): an impact check reads until the call that
// runs it must answer, not until a fixed 30 s. The session budget
// (relaySessionOverallTimeout) still bounds every other phase. A check the
// deadline cuts stays typed (LIMIT_CUT, "read budget") and says it ran out
// of time.
const (
	cliCheckBudget = 90 * time.Second // a CLI command: Claude Code's 120 s command limit
	mcpCheckBudget = 45 * time.Second // an MCP tool call: the common 60 s limit
	chatCallBudget = 55 * time.Second // a chat app gives up about a minute after its request
)

// checkDeadline: when the running call's checks must end; zero keeps the
// session budget (tests, and a long-running mcp or serve between calls).
var checkDeadline time.Time

// chatCheckDeadline: a chat-app request answers about a minute after it
// came in (since), whatever it waited for the lock.
func chatCheckDeadline(since time.Time) time.Time { return since.Add(chatCallBudget) }

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// readUntilDeadline extends the session's read budget for one check to
// checkDeadline, and never to less than a session budget from now (a check
// after a long wait, such as apply's after the window, reads as it did
// before #1009). Earlier contexts stay alive until close, as in renewBudget.
func (s *relaySession) readUntilDeadline() {
	if checkDeadline.IsZero() {
		return
	}
	until := later(checkDeadline, time.Now().Add(relaySessionOverallTimeout))
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, set := s.ctx.Deadline(); set && !until.After(current) {
		return
	}
	previous := s.cancel
	ctx, cancel := context.WithDeadline(context.Background(), until)
	s.ctx, s.cancel = ctx, func() { cancel(); previous() }
}
