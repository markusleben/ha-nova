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

// Who may use chat apps, and the owner's switches (chat-apps.json, written
// by the NOVA App), read on every request.

// policyFor: the owner's current switches for user, or why they may not
// use chat apps. Read on every request, and again before a phone tap
// applies (mcp_serve_phone.go).
func (b *chatBridge) policyFor(user string) (*chatPolicy, int, string) {
	settings, err := readChatSettings(b.dataDir)
	if err != nil {
		b.log.Printf("refusing every request: %s", err)
		return nil, http.StatusServiceUnavailable, "HA NOVA chat-app settings are unreadable"
	}
	allowed := settings.AllowedUsers
	if allowed == nil {
		owner, err := b.ownerID()
		if err != nil {
			return nil, http.StatusServiceUnavailable, "HA NOVA can't read the Home Assistant owner right now"
		}
		allowed = &[]string{owner}
	}
	switch {
	case slices.Contains(*allowed, user):
	case settings.AllowedUsers == nil:
		return nil, http.StatusForbidden, "Only the Home Assistant owner can use HA NOVA in chat apps. Sign in as the owner."
	default:
		return nil, http.StatusForbidden, "This Home Assistant user can't use HA NOVA in chat apps. Sign in with an account the owner allowed."
	}
	return &chatPolicy{user: user, key: chatUserKey(user), links: b.linksFile(), codes: settings.ChatCodes == nil || *settings.ChatCodes,
		readOnly: settings.ReadOnly, bridge: b}, 0, ""
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
	ReadOnly     bool      `json:"read_only"`
	ChatCodes    *bool     `json:"chat_codes"` // absent: on
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
	return settings, nil
}

// ownerID reads the HA owner through the Relay, cached for a minute.
func (b *chatBridge) ownerID() (string, error) {
	cached := func() (string, error, bool) {
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case b.owner != "" && time.Since(b.ownerAt) < time.Minute:
			return b.owner, nil, true
		case time.Since(b.ownerNo) < 5*time.Second: // a failing Relay is not asked again per request
			return "", errors.New("the owner could not be read a moment ago"), true
		}
		return "", nil, false
	}
	if owner, err, ok := cached(); ok {
		return owner, err
	}
	mcpVerbLock.Lock() // a Relay session sets process-wide server selection
	defer mcpVerbLock.Unlock()
	if owner, err, ok := cached(); ok { // another request read it while this one waited
		return owner, err
	}
	owner, err := b.readOwner()
	b.mu.Lock()
	if err != nil {
		b.ownerNo = time.Now()
	} else {
		b.owner, b.ownerAt = owner, time.Now()
	}
	b.mu.Unlock()
	return owner, err
}

// readOwner: the active owner from config/auth/list (under mcpVerbLock).
func (b *chatBridge) readOwner() (string, error) {
	users, err := b.readUsers()
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.IsOwner && u.IsActive && !u.SystemGenerated && haUserIDPattern.MatchString(u.ID) {
			return u.ID, nil
		}
	}
	return "", errors.New("no active owner")
}
