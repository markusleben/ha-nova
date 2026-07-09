package main

import (
	"os"
	"path/filepath"
	"strings"
)

// claudeConfigRoot resolves the Claude Code state root exactly like the
// claude CLI itself: CLAUDE_CONFIG_DIR when set, ~/.claude otherwise.
// ha-nova spawns `claude` subprocesses that honor this variable — reading a
// different root than they write makes every sync verify against the wrong
// state and roll back (live-hit: `ha-nova update` inside a Claude Code
// session, which exports CLAUDE_CONFIG_DIR).
func claudeConfigRoot(home string) string {
	if root := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); root != "" {
		return root
	}
	return filepath.Join(home, ".claude")
}

// claudeConfigRootRedirected reports whether CLAUDE_CONFIG_DIR points the
// claude CLI away from the default ~/.claude root.
func claudeConfigRootRedirected(home string) bool {
	return claudeConfigRoot(home) != filepath.Join(home, ".claude")
}
