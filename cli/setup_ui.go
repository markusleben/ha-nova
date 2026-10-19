package main

import (
	"fmt"
	"io"
	"strings"
)

const (
	setupIssueGeneric          = "generic"
	setupIssueRelayUnreachable = "relay_unreachable"
	setupIssueWSDegraded       = "ws_degraded"
	setupIssueSkillsInstall    = "skills_install"
	setupIssueCloudAccess      = "cloud_access"
)

func hasAvailableSetupClientChoice(choices []setupClientChoice) bool {
	for _, choice := range choices {
		if !choice.Disabled {
			return true
		}
	}
	return false
}

func renderSetupHeader(out io.Writer) {
	session := resolveSetupUISession(out)
	clearSetupScreen(out)
	renderSimpleHeader(out, session, setupT("setup.header"))
}

func clearSetupScreen(out io.Writer) {
	if !resolveSetupUISession(out).clearsScreen() {
		return
	}
	fmt.Fprint(out, "\033[2J\033[H")
}

func renderSetupStep(out io.Writer, step, total int, title string) {
	session := resolveSetupUISession(out)
	stepLabel := setupT("setup.progress", "step", step, "total", total, "title", title)
	if session.plain() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  [%d/%d] %s\n", step, total, stepLabel)
		return
	}

	bar := strings.Builder{}
	for idx := 1; idx <= total; idx++ {
		if idx <= step {
			bar.WriteString("●")
		} else {
			bar.WriteString("○")
		}
		if idx < total {
			bar.WriteString(" ")
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s  %s\n", session.style("accent", bar.String()), session.style("strong", stepLabel))
}

func renderSetupStatusSummary(out io.Writer, state setupState) {
	session := resolveSetupUISession(out)
	check := func(ok bool, text string) {
		role, marker := "error", session.errorMarker()
		if ok {
			role, marker = "success", session.successMarker()
		}
		fmt.Fprintf(out, "  %s %s\n", session.style(role, marker), text)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", setupT("setup.status.checking")))
	if state.RelayOK {
		check(true, setupT("setup.status.app_reachable"))
		check(true, setupT("setup.status.auth_valid"))
	} else {
		check(false, setupT("setup.status.app_unreachable"))
		if state.TokenOK {
			check(false, setupT("setup.status.auth_failed"))
		} else {
			check(false, setupT("setup.status.no_token"))
		}
	}
	if state.WSOK {
		check(true, setupT("setup.status.ws_connected"))
	} else {
		check(false, setupT("setup.status.ws_disconnected"))
	}
	if state.SkillsOK {
		check(true, setupT("setup.status.skills_installed"))
	} else {
		check(false, setupT("setup.status.skills_missing"))
	}
	fmt.Fprintln(out)
}

func renderSetupDiscoveryResult(out io.Writer, host, via string, discovered bool) {
	session := resolveSetupUISession(out)
	success := session.style("success", session.successMarker())
	warning := session.style("warning", session.warningMarker())
	if discovered && via != "" {
		fmt.Fprintf(out, "  %s %s\n", success, setupT("setup.discovery.found_via", "host", host, "via", via))
	} else if discovered && isLocalDiscoveryHost(host) {
		fmt.Fprintf(out, "  %s %s\n", warning, setupT("setup.discovery.found_local", "host", host))
		fmt.Fprintf(out, "    %s\n", setupT("setup.discovery.local_name_fragile"))
		fmt.Fprintf(out, "    %s\n", setupT("setup.discovery.find_ip"))
	} else if discovered {
		fmt.Fprintf(out, "  %s %s\n", success, setupT("setup.discovery.found", "host", host))
	} else if strings.TrimSpace(host) != "" {
		fmt.Fprintf(out, "  %s %s\n", warning, setupT("setup.discovery.none_saved", "host", host))
	} else {
		fmt.Fprintf(out, "  %s %s\n", warning, setupT("setup.discovery.none"))
	}
}

// look, when present, replaces the generic example prompts with the first
// look (setup_first_look.go).
func renderSetupCompleteBanner(out io.Writer, clients []string, look *setupFirstLook) {
	session := resolveSetupUISession(out)
	renderSetupHeader(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s %s\n", session.style("success", session.successMarker()), setupT("setup.done.complete"))
	fmt.Fprintln(out)
	if labels := setupClientLabels(clients); labels != "" {
		fmt.Fprintf(out, "  %s\n", setupStyled(session, "setup.done.installed_for", "clients", setupStrong(labels)))
		fmt.Fprintln(out)
	}
	promptHeader := setupStyled(session, "setup.done.try_asking_in", "client", setupStrong(setupClientLabelsForPrompt(clients)))
	if look != nil {
		renderSetupFirstLook(out, look, promptHeader)
	} else {
		fmt.Fprintf(out, "  %s\n", promptHeader)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "    \"%s\"\n", setupT("setup.done.example_light"))
		fmt.Fprintf(out, "    \"%s\"\n", setupT("setup.done.example_automations"))
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  %s\n", setupT("setup.done.need_help"))
	fmt.Fprintln(out)
}

func renderSetupAlreadyDoneBanner(out io.Writer, legacyToken bool) {
	session := resolveSetupUISession(out)
	renderSetupHeader(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s %s\n", session.style("success", session.successMarker()), setupT("setup.done.already"))
	fmt.Fprintln(out)
	if legacyToken {
		// The upgrade path must not dead-end here (issue #419): a working
		// legacy-token install passes every completeness check, so this screen
		// is exactly where the pairing switch has to be offered.
		renderSetupIndentedLines(out, "  ", setupT("setup.done.legacy_token"))
		renderSetupIndentedLines(out, "  ", strings.Split(setupT("setup.done.legacy_switch"), "\n")...)
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  %s\n", setupT("setup.done.doctor"))
	fmt.Fprintln(out)
}

func renderSetupCloudFallbackReadyBanner(out io.Writer, look *setupFirstLook) {
	session := resolveSetupUISession(out)
	renderSetupHeader(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s %s\n", session.style("success", session.successMarker()), setupT("setup.done.complete"))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", setupT("setup.done.connections"))
	renderSetupIndentedLines(out, "    ",
		setupT("setup.done.connection_local"),
		setupT("setup.done.connection_away"),
		setupT("setup.done.connection_routing"),
	)
	fmt.Fprintln(out)
	if look != nil {
		renderSetupFirstLook(out, look, setupT("setup.done.try_asking"))
	}
	fmt.Fprintf(out, "  %s\n", setupT("setup.done.doctor"))
	fmt.Fprintln(out)
}

func renderSetupIncompleteBanner(out io.Writer, issue string) {
	session := resolveSetupUISession(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s %s\n", session.style("error", session.errorMarker()), setupT("setup.incomplete.title"))
	fmt.Fprintln(out)
	var lines []string
	switch issue {
	case setupIssueWSDegraded:
		lines = []string{setupT("setup.incomplete.ws_degraded"), setupT("setup.incomplete.ws_degraded_fix")}
	case setupIssueRelayUnreachable:
		lines = []string{setupT("setup.incomplete.relay_unreachable"), setupT("setup.incomplete.relay_unreachable_fix")}
	case setupIssueSkillsInstall:
		lines = []string{setupT("setup.incomplete.skills"), setupT("setup.incomplete.skills_fix")}
	case setupIssueCloudAccess:
		lines = []string{setupT("setup.incomplete.link"), setupT("setup.incomplete.link_fix")}
	default:
		lines = []string{setupT("setup.incomplete.generic")}
	}
	if issue != setupIssueSkillsInstall &&
		issue != setupIssueCloudAccess {
		lines = append(lines, setupT("setup.incomplete.rerun"))
	}
	renderSetupIndentedLines(out, "  ", lines...)
	fmt.Fprintln(out)
}

func setupClientLabel(client string) string {
	switch client {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex CLI"
	case "opencode":
		return "OpenCode"
	case "antigravity":
		return "Google Antigravity"
	case "hermes":
		return "Hermes Agent"
	case "all":
		return setupT("setup.clients.all_available")
	default:
		return setupT("setup.clients.your_assistant")
	}
}

func setupClientLabels(clients []string) string {
	if len(clients) == 0 {
		return ""
	}
	labels := make([]string, 0, len(clients))
	for _, client := range clients {
		labels = append(labels, setupClientLabel(client))
	}
	return strings.Join(labels, ", ")
}

func setupClientLabelsForPrompt(clients []string) string {
	if len(clients) == 0 {
		return setupT("setup.clients.your_assistant")
	}
	if len(clients) == 1 {
		return setupClientLabel(clients[0])
	}
	return setupT("setup.clients.your_installed")
}
