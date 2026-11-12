package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The old shared token: the NOVA App no longer accepts it (#905). Every place
// a person (or their AI assistant) meets HA NOVA says how to pair again in the
// same words: setup, doctor, update, and check-update (each AI session's first
// HA task) while an older App still takes the token, and every refusal from an
// App that no longer does. Typed-verb/MCP results carry the refusal only.
const sharedAccessEndedFormat = "This computer still uses the old shared access, which the updated NOVA App no longer accepts. %s"

// relayPairingRequiredHeader marks the NOVA App's 401 to the old shared token
// (nova/src/runtime/listeners.ts), so a refusal is told apart without reading
// the body.
const relayPairingRequiredHeader = "X-Ha-Nova-Auth"

// errSharedAccessEnded: a health check met that refusal. Its text keeps
// "HTTP 401", so setup still reads it as a sign-in problem.
var errSharedAccessEnded = errors.New("HTTP 401: the NOVA App no longer accepts the old shared access")

// sharedAccessEnded: the local NOVA App refused the old shared token.
func sharedAccessEnded(status int, header http.Header) bool {
	return status == http.StatusUnauthorized && header.Get(relayPairingRequiredHeader) == "pairing-required"
}

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
	return sharedAccessEndedFor(cfg, defaultServerProfileName)
}

// sharedAccessEndedFor: how this computer pairs again. A service install's
// setup keeps its token, so it pairs file-backed; a named server pairs with
// pair --server; pairing waits until Home Assistant Link is removed, and Link
// is added again after it.
func sharedAccessEndedFor(cfg runtimeConfig, profile string) string {
	command := localRelayRepairCommand(profile, cfg.RelayBaseURL)
	if strings.TrimSpace(cfg.RelayTokenFile) != "" {
		command = "ha-nova pair --credential-store=file"
	}
	next := fmt.Sprintf("Run %s in a terminal once (takes a minute) to pair this computer again.", command)
	if cfg.Cloud != nil {
		next = fmt.Sprintf("Remove Home Assistant Link first (%s), then run %s in a terminal to pair this computer again. Add Link again afterwards if you use it (%s).", cloudProfileCommandFor("remove", profile), command, cloudProfileCommandFor("add", profile))
	}
	return fmt.Sprintf(sharedAccessEndedFormat, next)
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

// legacyTokenReadable: the notice needs the shared token itself, read
// without a prompt (a missing or unreadable one is doctor's error to name).
func legacyTokenReadable() bool {
	token, err := readRelayAuthTokenForDoctor()
	return err == nil && strings.TrimSpace(token) != ""
}

// printLegacyAccessNotice prints it; agent asks the AI assistant to pass it
// on once (the person runs the command in their own terminal).
func printLegacyAccessNotice(ctx context.Context, paths runtimePaths, agent bool) {
	printLegacyAccessNoticeText(legacyAccessNotice(ctx, paths), agent)
}

func printLegacyAccessNoticeText(notice string, agent bool) {
	switch {
	case notice == "":
	case agent:
		printHumanWarn("Tell the user once, after the result: %s They run it in their own terminal; never run it for them.", notice)
	default:
		printHumanWarn("%s", notice)
	}
}
