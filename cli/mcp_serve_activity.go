package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// What chat apps did, for the owner's NOVA page (docs/work/2026-10-24-chat-
// apps-household-spec.md, Activity): activity.jsonl, one line per change a
// chat app made or tried (who, when, the service or plan kind, the target
// names, how it ended), never a read, a chat text, a code, or service data;
// and seen.json, the hour of each user's last request. Both are the
// bridge's own (0600), and only the control channel reads them.

const (
	activityMax     = 500
	activityRefused = 100
	activityKeep    = 30 * 24 * time.Hour
	activityPage    = 20
)

type activityLine struct {
	At      string   `json:"at"`
	User    string   `json:"user"`
	Name    string   `json:"name"` // the user's name in Home Assistant at that moment
	Tool    string   `json:"tool"` // ha_call or ha_apply
	What    string   `json:"what"` // the service, or the plan kind
	Targets []string `json:"targets,omitempty"`
	Result  string   `json:"result"` // done, phone, code, declined, refused, failed, unknown
}

// noteChange records a chat app's change (ha_call, ha_apply) as it ended,
// a typed one as confirmed by how (phone or code); a preview, a wait for
// the phone, no_change, and invalid arguments add no line. Trimmed to the
// last 500 lines of the last 30 days on each write, at most 100 of them
// refusals: anyone allowed can be refused, and that never pushes out what ran.
func (b *chatBridge) noteChange(p *chatPolicy, tool, what, how string, result map[string]any) {
	env, _ := result["structuredContent"].(map[string]any)
	ended := activityResult(env, how)
	if ended == "" || p == nil {
		return
	}
	now := time.Now().UTC()
	line := activityLine{At: now.Format(time.RFC3339), User: p.user, Name: p.name, Tool: tool, What: what, Targets: activityTargets(env), Result: ended}
	b.activityMu.Lock()
	defer b.activityMu.Unlock()
	lines := append(b.readActivity(), line)
	for i, refused := len(lines)-1, 0; i >= 0; i-- {
		if lines[i].Result == "refused" {
			if refused++; refused > activityRefused {
				lines = slices.Delete(lines, i, i+1)
			}
		}
	}
	var out bytes.Buffer
	for _, l := range lines[max(0, len(lines)-activityMax):] {
		if at, err := time.Parse(time.RFC3339, l.At); err == nil && now.Sub(at) < activityKeep {
			raw, _ := json.Marshal(l)
			out.Write(append(raw, '\n'))
		}
	}
	if err := writeFileAtomic(filepath.Join(b.dataDir, "activity.jsonl"), out.Bytes(), 0o600); err != nil {
		b.log.Printf("activity: %v", err)
	}
}

// activityResult: how a change's envelope ended; "" when it isn't one.
func activityResult(env map[string]any, how string) string {
	status, _ := env["status"].(string)
	errInfo, _ := env["error"].(map[string]any)
	code, _ := errInfo["code"].(string)
	plan, _ := env["plan"].(map[string]any)
	switch {
	case env == nil, status == "needs_confirmation", status == "waiting_for_phone", status == "no_change", code == "INVALID_ARGUMENTS":
		return ""
	case env["outcome_unknown"] == true, status == "unverified":
		return "unknown"
	case code == "CONFIRM_DECLINED":
		return "declined"
	case status == "failed", status == "verification_failed":
		return "failed"
	case errInfo != nil || status == "error":
		return "refused"
	case plan["confirm"] == "code": // typed: a phone tap in the phone's job, else a chat code
		return how
	}
	return "done"
}

// activityTargets: the resolved targets' names (entity ids where none), at
// most 10.
func activityTargets(env map[string]any) []string {
	resolved, _ := env["resolved"].([]any)
	var names []string
	for _, r := range resolved[:min(len(resolved), 10)] {
		target, _ := r.(map[string]any)
		name, _ := target["name"].(string)
		if name == "" {
			name, _ = target["entity_id"].(string)
		}
		names = append(names, terminalSafeName(name))
	}
	return names
}

// readActivity: the lines on file; an unreadable line is dropped.
func (b *chatBridge) readActivity() []activityLine {
	raw, err := os.ReadFile(filepath.Join(b.dataDir, "activity.jsonl"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		b.log.Printf("activity: %v", err)
	}
	var lines []activityLine
	scan := bufio.NewScanner(bytes.NewReader(raw))
	for scan.Scan() {
		var l activityLine
		if json.Unmarshal(scan.Bytes(), &l) == nil {
			lines = append(lines, l)
		}
	}
	return lines
}

// controlActivity: the last 20 changes, newest first.
func (b *chatBridge) controlActivity() (int, map[string]any) {
	b.activityMu.Lock()
	lines := b.readActivity()
	b.activityMu.Unlock()
	changes := []activityLine{}
	for i := len(lines) - 1; i >= 0 && len(changes) < activityPage; i-- {
		changes = append(changes, lines[i])
	}
	return http.StatusOK, map[string]any{"changes": changes}
}

// noteSeen keeps the hour of user's last request (written when it moves).
func (b *chatBridge) noteSeen(user string) {
	hour := time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)
	b.activityMu.Lock()
	defer b.activityMu.Unlock()
	if b.seen == nil {
		b.seen = b.readSeen()
	}
	if b.seen[user] == hour {
		return
	}
	b.seen[user] = hour
	raw, _ := json.Marshal(b.seen)
	if err := writeFileAtomic(filepath.Join(b.dataDir, "seen.json"), raw, 0o600); err != nil {
		b.log.Printf("seen: %v", err)
	}
}

func (b *chatBridge) readSeen() map[string]string {
	seen := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(b.dataDir, "seen.json")); err == nil {
		_ = json.Unmarshal(raw, &seen)
	}
	if seen == nil { // a file holding null
		seen = map[string]string{}
	}
	return seen
}

// controlSeen: each user's last request, by the hour.
func (b *chatBridge) controlSeen() (int, map[string]any) {
	b.activityMu.Lock()
	defer b.activityMu.Unlock()
	if b.seen == nil {
		b.seen = b.readSeen()
	}
	return http.StatusOK, map[string]any{"users": maps.Clone(b.seen)}
}

// chatWhat: what a chat app's change names, read before it runs (apply
// uses its plan up); "" for anything but a change.
func (srv *mcpServer) chatWhat(name string, args map[string]json.RawMessage) string {
	service := text(args, "service")
	switch {
	case srv.chat == nil:
		return ""
	case name == "ha_call" && len(service) <= 255 && entityIDPattern.MatchString(service): // a service's name, never a chat's text (an invalid one runs nothing: no line)
		return service
	case name != "ha_apply":
		return ""
	}
	if id := text(args, "plan_id"); planIDPattern.MatchString(id) {
		if plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json")); err == nil {
			return planWhat(plan)
		}
	}
	return "plan"
}

// noteChange: a chat app's change as it ended, in the bridge's activity;
// typed and not in a phone's job (mcp_serve_phone.go): with a chat code.
func (srv *mcpServer) noteChange(name, what string, result map[string]any) {
	if srv.chat != nil && what != "" {
		srv.chat.bridge.noteChange(srv.chat, name, what, "code", result)
	}
}

// planWhat: a call plan's service, else the plan's kind.
func planWhat(plan storedPlan) string {
	if plan.Kind == "call" {
		return plan.Request.Service
	}
	return plan.Kind
}
