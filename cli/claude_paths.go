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

// warnClaudeProfileRedirect: the sync landed in a CLAUDE_CONFIG_DIR profile.
// A default Claude Code at ~/.claude is a separate install that this sync
// did not touch: say so when it has no HA NOVA, or an older one (the shared
// ClientsVerifiedVersion means no self-heal would fix it). Silent otherwise.
// stderr without a WARNING label: news, and stdout may carry JSON.
func warnClaudeProfileRedirect(paths runtimePaths) {
	if !claudeConfigRootRedirected(paths.Home) {
		return
	}
	line := ""
	switch claudeHANovaAt(filepath.Join(paths.Home, ".claude"), localVersion(paths)) {
	case claudeHANovaNone:
		// Claude Code 2.1.x can leave an install out of its file: only its
		// own answer for ~/.claude says HA NOVA is missing there.
		if found, ok := defaultClaudeListsHANova(paths.Home); found || !ok {
			return
		}
		line = "Your other Claude Code setup in ~/.claude doesn't have HA NOVA yet. To add it there, run `ha-nova setup claude` in a terminal without CLAUDE_CONFIG_DIR."
	case claudeHANovaOlder:
		line = "Your other Claude Code setup in ~/.claude still has an older HA NOVA. To update it there, run `ha-nova update` in a terminal without CLAUDE_CONFIG_DIR."
	default:
		return
	}
	printHumanNotice(humanNotice{level: humanNoticeNote, message: line})
}

// defaultClaudeListsHANova asks `claude plugin list --json` about ~/.claude
// itself (CLAUDE_CONFIG_DIR left out); ok=false: no answer. Test seam.
var defaultClaudeListsHANova = func(home string) (found, ok bool) {
	env := []string{"HOME=" + home}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.EqualFold(key, "CLAUDE_CONFIG_DIR") && !strings.EqualFold(key, "HOME") {
			env = append(env, kv)
		}
	}
	found, _, ok = readClaudePluginListWithEnv(env)
	return found, ok
}

const (
	claudeHANovaUnknown = "" // current, or the state can't be read
	claudeHANovaNone    = "none"
	claudeHANovaOlder   = "older"
)

// claudeHANovaAt: HA NOVA in the Claude Code root `root`: none (the root
// exists, its plugin list has no HA NOVA), older (a record older than
// version; the plugin carries the release's version without "-rcN"), or
// unknown. No root, or unreadable state, is unknown.
func claudeHANovaAt(root, version string) string {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return claudeHANovaUnknown
	}
	data, err := os.ReadFile(filepath.Join(root, "plugins", "installed_plugins.json"))
	if isNotExist(err) {
		return claudeHANovaNone
	}
	var raw map[string]any
	if err != nil || unmarshalClaudeJSON(data, &raw) != nil {
		return claudeHANovaUnknown
	}
	plugins, keyed := raw["plugins"].(map[string]any) // the current layout: keyed by plugin id
	if !keyed {
		if claudeInstalledPluginRecordPresent(raw["plugins"]) {
			return claudeHANovaUnknown
		}
		return claudeHANovaNone
	}
	records, has := plugins["ha-nova@ha-nova"]
	list, isList := records.([]any)
	if !has || isList && len(list) == 0 {
		return claudeHANovaNone
	}
	// Older only when no record is current: a leftover project record
	// beside a current one says nothing.
	want, _, _ := strings.Cut(version, "-")
	older := false
	for _, record := range list {
		entry, _ := record.(map[string]any)
		got, _ := entry["version"].(string)
		cmp, err := compareReleaseVersions(got, want)
		if err != nil || cmp >= 0 {
			return claudeHANovaUnknown
		}
		older = true
	}
	if older {
		return claudeHANovaOlder
	}
	return claudeHANovaUnknown
}
