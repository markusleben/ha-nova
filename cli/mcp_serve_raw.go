package main

import (
	"encoding/json"
	"path/filepath"
)

// Home Assistant's own config APIs (automations, scripts, dashboards,
// registries) need an administrator, and a chat app is never wider than
// Home Assistant (docs/work/2026-10-24-chat-apps-household-spec.md): every
// plan but a service call (ha_plan kinds write, delete, revert, restore,
// rooms, template, raw) is for the Home Assistant owner or an administrator
// only, and ha_apply of such a plan checks again (also after a form or a
// phone tap), so a user demoted since planning can't run it.

// chatAdminRefusal is configAdminFailure as a tool result; nil when allowed.
func (srv *mcpServer) chatAdminRefusal(name string, args map[string]json.RawMessage) map[string]any {
	failure := srv.configAdminFailure(name, args)
	if failure == nil {
		return nil
	}
	return chatStopped(name, refusedWith(failure.code, failure.message, failure.next))
}

// configAdminFailure: why this chat-app call may not plan or apply a
// config plan (its message without "nothing was sent"), or nil: not a chat
// app, a call plan, or a user who is an owner or administrator right now.
// Fails closed when the users can't be read.
func (srv *mcpServer) configAdminFailure(name string, args map[string]json.RawMessage) *verbFailure {
	c := srv.chat
	if c == nil {
		return nil
	}
	switch name {
	case "ha_plan":
		if text(args, "kind") == "call" {
			return nil
		}
	case "ha_apply":
		id := text(args, "plan_id")
		if !planIDPattern.MatchString(id) {
			return nil
		}
		if plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json")); err != nil || plan.Kind == "call" {
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
