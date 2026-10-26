package main

import (
	"context"
	"strings"
	"time"
)

// The shared legacy token: 0.27 is the last release that accepts it. Every
// place a person (or their AI assistant) meets HA NOVA says so in the same
// words: setup, doctor, update, and check-update (each AI session's first
// HA task). Typed-verb/MCP results don't: read verbs send only reads.
const legacyAccessNoticeText = "This computer still uses the old shared access. It works today, but the next HA NOVA release won't accept it. Run ha-nova setup once (takes a minute) to switch."

// A service install's setup keeps its token; its switch is the file-backed pair.
const legacyAccessServiceNoticeText = "This computer still uses the old shared access. It works today, but the next HA NOVA release won't accept it. Run ha-nova pair --credential-store=file once (takes a minute) to switch."

// legacyNoticeProbeTimeout bounds the notice's probe where a person waits
// (doctor, update).
const legacyNoticeProbeTimeout = 2 * time.Second

// legacySharedAccess: the default server connects with the shared token
// (no pinned secure endpoint, no pairing waiting to finish), as
// `server list` shows "no (legacy token)".
func legacySharedAccess(cfg runtimeConfig) bool {
	return activeServerProfile() == defaultServerProfileName &&
		strings.TrimSpace(cfg.RelaySecureBaseURL) == "" &&
		strings.TrimSpace(cfg.PendingSecureBaseURL) == "" &&
		strings.TrimSpace(cfg.RelayBaseURL) != ""
}

func legacyAccessNoticeFor(cfg runtimeConfig) string {
	if strings.TrimSpace(cfg.RelayTokenFile) != "" {
		return legacyAccessServiceNoticeText
	}
	return legacyAccessNoticeText
}

// legacyAccessSwitchable: shared access, and its NOVA App can pair this
// computer instead. A standalone container can't pair yet, so it is never
// told to switch.
func legacyAccessSwitchable(cfg runtimeConfig, probe func(string) bool) bool {
	// Pairing refuses while Home Assistant Link is saved: never offer it then.
	return legacySharedAccess(cfg) && cfg.Cloud == nil && probe(cfg.RelayBaseURL)
}

// legacyAccessNotice: the notice for the selected server, probed within
// ctx; "" when it doesn't apply (paired, can't pair yet, uninstalled).
func legacyAccessNotice(ctx context.Context, paths runtimePaths) string {
	cfg, err := loadConfig(paths)
	if err != nil || installLifecycleStopped(paths) || !legacyTokenReadable() ||
		!legacyAccessSwitchable(cfg, func(url string) bool { return probePairingV1ForNotice(ctx, url) }) {
		return ""
	}
	return legacyAccessNoticeFor(cfg)
}

// legacyTokenReadable: "it works today" needs the shared token itself, read
// without a prompt (a missing or unreadable one is doctor's error to name).
func legacyTokenReadable() bool {
	token, err := readRelayAuthTokenForDoctor()
	return err == nil && strings.TrimSpace(token) != ""
}

// printLegacyAccessNotice prints it; agent asks the AI assistant to pass it
// on once (the person runs the command in their own terminal).
func printLegacyAccessNotice(ctx context.Context, paths runtimePaths, agent bool) {
	notice := legacyAccessNotice(ctx, paths)
	switch {
	case notice == "":
	case agent:
		printHumanWarn("Tell the user once, after the result: %s They run it in their own terminal; never run it for them.", notice)
	default:
		printHumanWarn("%s", notice)
	}
}
