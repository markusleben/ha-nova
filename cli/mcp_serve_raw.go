package main

import (
	"encoding/json"
	"path/filepath"
)

// Until household levels land (docs/work/2026-10-24-chat-apps-household-spec.md),
// a chat app's raw plan is for the Home Assistant owner or an administrator
// only: ha_plan kind raw is refused for anyone else, and ha_apply of a raw
// plan checks again (also after a form or a phone tap), so a user demoted
// since planning can't run it.

// chatRawRefusal is rawAdminFailure as a tool result; nil when allowed.
func (srv *mcpServer) chatRawRefusal(name string, args map[string]json.RawMessage) map[string]any {
	failure := srv.rawAdminFailure(name, args)
	if failure == nil {
		return nil
	}
	return chatStopped(name, refusedWith(failure.code, failure.message, failure.next))
}

// rawAdminFailure: why this chat-app call may not plan or apply a raw
// plan (its message without "nothing was sent"), or nil: not a chat app,
// not a raw plan, or a user who is an owner or administrator right now.
// Fails closed when the users can't be read.
func (srv *mcpServer) rawAdminFailure(name string, args map[string]json.RawMessage) *verbFailure {
	c := srv.chat
	if c == nil {
		return nil
	}
	switch name {
	case "ha_plan":
		if text(args, "kind") != "raw" {
			return nil
		}
	case "ha_apply":
		id := text(args, "plan_id")
		if !planIDPattern.MatchString(id) {
			return nil
		}
		if plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json")); err != nil || plan.Kind != "raw" {
			return nil // a missing plan: apply says so itself
		}
	default:
		return nil
	}
	mcpVerbLock.Lock() // a Relay session sets process-wide server selection
	users, err := c.bridge.readUsers()
	mcpVerbLock.Unlock()
	if err != nil {
		return &verbFailure{code: "ADMIN_UNKNOWN", message: "HA NOVA can't check right now whether this Home Assistant user is an administrator",
			next: "Tell the user nothing changed: HA NOVA couldn't check their Home Assistant account just now. They can try again in a moment."}
	}
	for _, u := range users {
		if u.ID == c.user && phoneAdmin(u) {
			return nil
		}
	}
	return &verbFailure{code: "ADMIN_ONLY", message: "from a chat app, only the Home Assistant owner or an administrator makes this kind of change",
		next: "Tell the user nothing changed: this change needs a Home Assistant administrator. An administrator can make it here or in Home Assistant itself."}
}

// readUsers: Home Assistant's users from config/auth/list (under
// mcpVerbLock).
func (b *chatBridge) readUsers() ([]haUser, error) {
	override, active := serverSelectionOverride, activeServerProfile()
	defer func() { setServerSelectionOverride(override); setActiveServerProfile(active) }()
	s, err := newRelaySession(b.paths, verbOptions{server: b.pin, serverSet: b.pin != ""})
	if err != nil {
		return nil, err
	}
	defer s.close()
	var users []haUser
	err = s.phoneRead("ws", map[string]any{"type": "config/auth/list"}, &users)
	return users, err
}
