package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The tools version and the ChatGPT refresh hint (docs/work/2026-11-10-
// chat-tools-version-spec.md): ChatGPT keeps the tool list and the
// instructions from its last Refresh. The version is a hash of both; when
// it changes, each known user's next tool result asks once for a Refresh,
// and the NOVA page shows a box. The version is in no tool description.

const (
	toolsVersionFile = "tools-version.json"
	toolsUserFile    = "tools-version"
	toolsRefreshHint = "Tell the user once, above any one-time code: HA NOVA was updated. If they use HA NOVA in ChatGPT, they open chatgpt.com/plugins, choose HA NOVA, then Refresh, so ChatGPT gets its new tools and instructions; Claude needs nothing."
)

type toolsVersionState struct {
	Version   string `json:"version"`
	ChangedAt string `json:"changed_at"` // RFC 3339; "": unchanged since this bridge's first start
}

// mcpToolsVersion: the first 12 hex digits of sha256 over the tools/list
// tools and the instructions.
func mcpToolsVersion(paths runtimePaths) string {
	tools, _ := json.Marshal(mcpToolList(paths))
	sum := sha256.Sum256(append(append(tools, 0), mcpInstructions...))
	return hex.EncodeToString(sum[:])[:12]
}

// noteToolsVersion records version in dataDir at start. A fresh bridge (no
// file, no user yet) has nothing to refresh; no file but users (an older
// App ran them) or another version: changed now.
func noteToolsVersion(dataDir, version string, now time.Time) (toolsVersionState, error) {
	path := filepath.Join(dataDir, toolsVersionFile)
	var state toolsVersionState
	raw, err := os.ReadFile(path)
	switch {
	case err == nil && json.Unmarshal(raw, &state) == nil && state.Version == version:
		return state, nil
	case errors.Is(err, os.ErrNotExist):
		users, _ := os.ReadDir(filepath.Join(dataDir, "users"))
		state = toolsVersionState{Version: version}
		if len(users) > 0 {
			state.ChangedAt = now.UTC().Format(time.RFC3339)
		}
	case err != nil:
		return state, err
	default:
		state = toolsVersionState{Version: version, ChangedAt: now.UTC().Format(time.RFC3339)}
	}
	raw, _ = json.Marshal(state)
	return state, writeFileAtomic(path, raw, 0o600)
}

// userDir creates a chat user's folder (their plans; key: chatUserKey). A
// new folder starts at the current tools version: nothing to refresh.
func (b *chatBridge) userDir(key string) (string, error) {
	dir := filepath.Join(b.dataDir, "users", key)
	b.mu.Lock() // a folder is never seen before its record (toolsHintFor)
	defer b.mu.Unlock()
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b.recordTools(dir)
	return dir, nil
}

// toolsHintFor: the hint for one request of the user whose folder is dir,
// "" for none. A folder with another version, or none (an older App made
// it), is due the hint, which one request at a time carries.
func (b *chatBridge) toolsHintFor(dir string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, _ := os.ReadFile(filepath.Join(dir, toolsUserFile))
	if b.pin == demoProfileName || strings.TrimSpace(string(raw)) == b.tools.Version || b.toolsDue[dir] {
		return ""
	}
	if b.toolsDue == nil {
		b.toolsDue = map[string]bool{}
	}
	b.toolsDue[dir] = true
	return toolsRefreshHint
}

// toolsHintDone ends a request that carried the hint: given, the version is
// recorded; not given, the next request carries it.
func (b *chatBridge) toolsHintDone(dir string, given bool) {
	if given {
		b.recordTools(dir)
	}
	b.mu.Lock()
	delete(b.toolsDue, dir)
	b.mu.Unlock()
}

func (b *chatBridge) recordTools(dir string) {
	if err := writeFileAtomic(filepath.Join(dir, toolsUserFile), []byte(b.tools.Version+"\n"), 0o600); err != nil {
		b.log.Printf("cannot record the tools version: %s", err)
	}
}

// addToolsHint puts the hint after a tool result's own next_step, its text
// rendered as before; a result without an envelope (a skill's text) gets it
// as a text of its own.
func (srv *mcpServer) addToolsHint(result map[string]any) {
	content, _ := result["content"].([]map[string]any)
	if srv.toolsHint == "" || srv.toolsHinted || len(content) == 0 {
		return
	}
	srv.toolsHinted = true
	envelope, _ := result["structuredContent"].(map[string]any)
	raw, _ := json.Marshal(envelope)
	plain, bare := envelope != nil && content[0]["text"] == string(ruleFirst(envelope)), envelope != nil && content[0]["text"] == string(raw)
	if !plain && !bare {
		result["content"] = append(content, map[string]any{"type": "text", "text": "next_step: " + srv.toolsHint})
		return
	}
	step, _ := envelope["next_step"].(string)
	envelope["next_step"] = strings.TrimSpace(step + " " + srv.toolsHint)
	if raw, _ = json.Marshal(envelope); plain {
		raw = ruleFirst(envelope)
	}
	content[0]["text"] = string(raw)
}

func (b *chatBridge) controlTools() (int, map[string]any) {
	return http.StatusOK, map[string]any{"version": b.tools.Version, "changed_at": b.tools.ChangedAt}
}
