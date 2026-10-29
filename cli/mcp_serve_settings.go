package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Who may use chat apps, at which level, and the owner's switches
// (chat-apps.json, written by the NOVA App), read on every request.

const chatNotAllowed = "This Home Assistant user can't use HA NOVA in chat apps. Sign in with an account the owner allowed."

// policyFor: the owner's current switches and this user's level, or why
// they may not use chat apps, with Home Assistant's users as read within
// the last minute. Read on every request, and again before a phone tap
// applies (mcp_serve_phone.go).
func (b *chatBridge) policyFor(user string) (*chatPolicy, int, string) { return b.policy(user, false) }

// policy: policyFor, with the users read now when fresh (before a change).
// The level is the lower of the owner's choice and the user's Home
// Assistant role (roleCap); no choice stored: the role's.
func (b *chatBridge) policy(user string, fresh bool) (*chatPolicy, int, string) {
	settings, err := readChatSettings(b.dataDir)
	if err != nil {
		b.log.Printf("refusing every request: %s", err)
		return nil, http.StatusServiceUnavailable, "HA NOVA chat-app settings are unreadable"
	}
	listed := settings.AllowedUsers != nil
	if listed && !slices.Contains(*settings.AllowedUsers, user) { // before any read: never reaches the Relay
		return nil, http.StatusForbidden, chatNotAllowed
	}
	users, err := b.homeUsers(fresh)
	owner := activeOwner(users)
	switch {
	case err != nil && !listed, !listed && owner == nil:
		return nil, http.StatusServiceUnavailable, "HA NOVA can't read the Home Assistant owner right now"
	case err != nil:
		return nil, http.StatusServiceUnavailable, "HA NOVA can't read this home's Home Assistant users right now"
	case !listed && owner.ID != user:
		return nil, http.StatusForbidden, "Only the Home Assistant owner can use HA NOVA in chat apps. Sign in as the owner."
	}
	i := slices.IndexFunc(users, func(u haUser) bool { return u.ID == user })
	if i < 0 || !users[i].IsActive || users[i].SystemGenerated {
		return nil, http.StatusForbidden, chatNotAllowed
	}
	granted := levelManage
	if level, stored := settings.Levels[user]; stored {
		granted = level
	}
	role := roleCap(users[i])
	p := &chatPolicy{user: user, key: chatUserKey(user), links: b.linksFile(), codes: settings.ChatCodes == nil || *settings.ChatCodes,
		readOnly: settings.ReadOnly, bridge: b, granted: granted, role: role, level: min(granted, role), name: terminalSafeName(users[i].Name)}
	if owner != nil {
		p.owner, p.ownerName = owner.ID == user, terminalSafeName(owner.Name)
	}
	return p, 0, ""
}

// reread: p with chat-apps.json read again and its role as read before,
// for the verb that runs under mcpVerbLock: a change the owner saved while
// the request waited for the lock holds. No longer allowed, or unreadable:
// level zero and read only, which refuses every change. nil: not a chat app.
func (p *chatPolicy) reread() *chatPolicy {
	if p == nil || p.bridge == nil {
		return p
	}
	now := *p
	settings, err := readChatSettings(p.bridge.dataDir)
	listed := settings.AllowedUsers != nil
	if err != nil || (listed && !slices.Contains(*settings.AllowedUsers, p.user)) || (!listed && !p.owner) {
		now.level, now.readOnly, now.codes = 0, true, false
		return &now
	}
	now.granted = levelManage
	if level, stored := settings.Levels[p.user]; stored {
		now.granted = level
	}
	now.level, now.readOnly, now.codes = min(now.granted, p.role), settings.ReadOnly, settings.ChatCodes == nil || *settings.ChatCodes
	return &now
}

// activeOwner: the active owner among users; nil when none.
func activeOwner(users []haUser) *haUser {
	for i, u := range users {
		if u.IsOwner && u.IsActive && !u.SystemGenerated && haUserIDPattern.MatchString(u.ID) {
			return &users[i]
		}
	}
	return nil
}

// chatUserKey: a user's data directory name and log label.
func chatUserKey(user string) string {
	sum := sha256.Sum256([]byte("ha-nova/chat-user/v1\x00" + user))
	return hex.EncodeToString(sum[:16])
}

// linksFile: the chat-app phone links, {HA user id: phoneLink}; only the
// bridge writes it (mcp_serve_control.go).
func (b *chatBridge) linksFile() string { return filepath.Join(b.dataDir, "phone-links.json") }

// readServeSecret: a secret file holds exactly 64 lowercase hex characters
// and is readable by its owner only; returns its sha256.
func readServeSecret(file, what string) ([32]byte, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return [32]byte{}, fmt.Errorf("cannot read the %s file: %w", what, err)
	}
	if info, err := os.Stat(file); err == nil && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return [32]byte{}, fmt.Errorf("the %s file must be readable by its owner only (chmod 600)", what)
	}
	secret := strings.TrimSpace(string(raw))
	if !serveSecretPattern.MatchString(secret) {
		return [32]byte{}, fmt.Errorf("the %s file must hold exactly 64 lowercase hex characters (256 bits)", what)
	}
	return sha256.Sum256([]byte(secret)), nil
}

// chatSettings is chat-apps.json, the owner's switches (written by the
// NOVA App). Missing: the defaults (the owner only, changes allowed, chat
// codes on). Unreadable or unknown fields: every request is refused.
type chatSettings struct {
	AllowedUsers *[]string `json:"allowed_users"` // absent: the owner only
	// Levels: an allowed user's level (ask, use, manage); absent: their
	// role's. Roles: 1 whenever the App writes a list, so a 0.27 bridge,
	// which refuses unknown fields, refuses the file after a rollback
	// instead of giving every listed user full power.
	Levels    map[string]chatLevel `json:"levels"`
	Roles     int                  `json:"roles"`
	ReadOnly  bool                 `json:"read_only"`
	ChatCodes *bool                `json:"chat_codes"` // absent: on
}

func readChatSettings(dataDir string) (chatSettings, error) {
	var settings chatSettings
	raw, err := os.ReadFile(filepath.Join(dataDir, "chat-apps.json"))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return settings, fmt.Errorf("chat-apps.json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF { // trailing data: never a partly read file
		return settings, errors.New("chat-apps.json: trailing data")
	}
	switch {
	case settings.Roles != 0 && settings.Roles != 1:
		return settings, fmt.Errorf("chat-apps.json: roles %d is from a newer NOVA App", settings.Roles)
	case settings.Levels != nil && settings.Roles != 1:
		return settings, errors.New("chat-apps.json: levels without roles")
	}
	return settings, nil
}

// homeUsers: Home Assistant's users (config/auth/list) through the Relay,
// as read within the last minute, or read now (fresh: before a change). A
// failed read fails closed; a failed cached read isn't repeated for 5 s,
// and a failed fresh one leaves the cache as it was.
func (b *chatBridge) homeUsers(fresh bool) ([]haUser, error) {
	cached := func() ([]haUser, error, bool) {
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case !fresh && time.Since(b.usersNo) < 5*time.Second: // a failing Relay is not asked again per request
			return nil, errors.New("the users could not be read a moment ago"), true
		case !fresh && !b.usersAt.IsZero() && time.Since(b.usersAt) < time.Minute:
			return b.users, nil, true
		}
		return nil, nil, false
	}
	if users, err, ok := cached(); ok {
		return users, err
	}
	mcpVerbLock.Lock() // a Relay session sets process-wide server selection
	defer mcpVerbLock.Unlock()
	if users, err, ok := cached(); ok && !fresh { // another request read them while this one waited
		return users, err
	}
	users, err := b.readUsers()
	b.mu.Lock()
	switch {
	case err == nil:
		b.users, b.usersAt, b.usersNo = users, time.Now(), time.Time{}
	case !fresh:
		b.usersNo = time.Now()
	}
	b.mu.Unlock()
	return users, err
}
