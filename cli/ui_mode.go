package main

import (
	"io"
	"os"
	"strings"
)

type uiMode string

const (
	uiModePlain    uiMode = "plain"
	uiModeStyled   uiMode = "styled"
	uiModeEnhanced uiMode = "enhanced"
)

type uiSession struct {
	mode  uiMode
	color bool
}

type humanNoticeLevel string

const (
	humanNoticeNone    humanNoticeLevel = ""
	humanNoticeInfo    humanNoticeLevel = "info"
	humanNoticeWarning humanNoticeLevel = "warning"
)

type humanNoticeKind string

const (
	humanNoticeKindNone              humanNoticeKind = ""
	humanNoticeKindUpToDate          humanNoticeKind = "up_to_date"
	humanNoticeKindUpdateAvailable   humanNoticeKind = "update_available"
	humanNoticeKindUpdateCheckFailed humanNoticeKind = "update_check_failed"
	humanNoticeKindChannelConflict   humanNoticeKind = "channel_conflict"
	humanNoticeKindLocalManifest     humanNoticeKind = "local_manifest"
	humanNoticeKindRelayOutdated     humanNoticeKind = "relay_outdated"
)

type humanNotice struct {
	level   humanNoticeLevel
	kind    humanNoticeKind
	message string
}

var uiEnvLookup = os.Getenv
var uiInputSupportsTTY = isInteractiveTTY
var uiOutputSupportsANSI = writerSupportsANSI

func resolveSetupUISession(out io.Writer) uiSession {
	if !uiInputSupportsTTY() {
		return uiSession{mode: uiModePlain}
	}
	return resolveUISession(out, writerSupportsTTYForSetup, true)
}

func resolveStatusUISession(out io.Writer) uiSession {
	return resolveUISession(out, writerSupportsTTY, false)
}

func resolveUISession(out io.Writer, outputTTY func(io.Writer) bool, allowEnhanced bool) uiSession {
	if uiPlainRequested() || !outputTTY(out) || !uiOutputSupportsANSI(out) {
		return uiSession{mode: uiModePlain}
	}

	mode := uiModeStyled
	if allowEnhanced && uiInputSupportsTTY() {
		mode = uiModeEnhanced
	}

	return uiSession{
		mode:  mode,
		color: true,
	}
}

func uiPlainRequested() bool {
	if strings.TrimSpace(uiEnvLookup("HA_NOVA_PLAIN_UI")) == "1" {
		return true
	}
	if uiEnvLookup("NO_COLOR") != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(uiEnvLookup("TERM")), "dumb") {
		return true
	}
	return false
}

func (u uiSession) plain() bool {
	return u.mode == uiModePlain
}

func (u uiSession) enhanced() bool {
	return u.mode == uiModeEnhanced
}

func (u uiSession) clearsScreen() bool {
	return !u.plain()
}

func (u uiSession) animatesSpinner() bool {
	return !u.plain()
}

func (u uiSession) successMarker() string {
	if u.plain() {
		return "[ok]"
	}
	return "✓"
}

func (u uiSession) warningMarker() string {
	if u.plain() {
		return "[!]"
	}
	return "▲"
}

func (u uiSession) errorMarker() string {
	if u.plain() {
		return "[!!]"
	}
	return "✗"
}

func (u uiSession) bullet() string {
	if u.plain() {
		return "-"
	}
	return "•"
}

func (u uiSession) style(role, text string) string {
	if !u.color || text == "" {
		return text
	}
	code := ""
	switch role {
	case "strong":
		code = "1"
	case "muted":
		code = "2"
	case "accent":
		code = "93"
	case "success":
		code = "32"
	case "warning":
		code = "33"
	case "error":
		code = "31"
	default:
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func printHumanInfo(format string, args ...interface{}) {
	message := formatMessage(format, args...)
	session := resolveStatusUISession(os.Stdout)
	prefix := session.style("accent", "[ha-nova]")
	if prefix == "" {
		prefix = "[ha-nova]"
	}
	writeStyledLine(os.Stdout, prefix, message)
}

func printHumanWarn(format string, args ...interface{}) {
	message := formatMessage(format, args...)
	session := resolveStatusUISession(os.Stderr)
	prefix := session.style("warning", "[ha-nova] WARNING:")
	if prefix == "" {
		prefix = "[ha-nova] WARNING:"
	}
	writeStyledLine(os.Stderr, prefix, message)
}

func printHumanErr(format string, args ...interface{}) {
	message := formatMessage(format, args...)
	session := resolveStatusUISession(os.Stderr)
	prefix := session.style("error", "[ha-nova] ERROR:")
	if prefix == "" {
		prefix = "[ha-nova] ERROR:"
	}
	writeStyledLine(os.Stderr, prefix, message)
}

func (n humanNotice) empty() bool {
	return n.level == humanNoticeNone || strings.TrimSpace(n.message) == ""
}

func printHumanNotice(notice humanNotice) {
	if notice.empty() {
		return
	}
	switch notice.level {
	case humanNoticeInfo:
		printHumanInfo("%s", notice.message)
	default:
		printHumanWarn("%s", notice.message)
	}
}
