package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const (
	setupIssueGeneric          = "generic"
	setupIssueRelayUnreachable = "relay_unreachable"
	setupIssueWSDegraded       = "ws_degraded"
	setupIssueSkillsInstall    = "skills_install"
)

func promptSetupClient(in io.Reader, out io.Writer, choices []setupClientChoice, defaultClient string) (string, error) {
	return promptSetupClientFromReader(bufio.NewReader(in), out, choices, defaultClient)
}

func promptSetupClientFromReader(reader *bufio.Reader, out io.Writer, choices []setupClientChoice, defaultClient string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no setup clients available")
	}
	if !hasAvailableSetupClientChoice(choices) {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  Which AI client do you use?")
		fmt.Fprintln(out)
		for _, choice := range choices {
			fmt.Fprintf(out, "    %s) %s\n", choice.Number, choice.Label)
			if choice.Disabled && choice.DisabledReason != "" {
				fmt.Fprintf(out, "       not available: %s\n", choice.DisabledReason)
			}
		}
		fmt.Fprintln(out)
		return "", fmt.Errorf("no supported AI clients detected on this machine yet")
	}
	defaultChoice := firstAvailableSetupClientChoice(choices)
	for _, choice := range choices {
		if choice.Value == defaultClient && !choice.Disabled {
			defaultChoice = choice
			break
		}
	}

	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  Which AI client do you use?")
		fmt.Fprintln(out)
		for _, choice := range choices {
			fmt.Fprintf(out, "    %s) %s\n", choice.Number, choice.Label)
			if choice.Disabled && choice.DisabledReason != "" {
				fmt.Fprintf(out, "       not available: %s\n", choice.DisabledReason)
			}
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  Enter [1-%d] (default %s, or type 'exit'): ", len(choices), defaultChoice.Number)

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		switch line {
		case "":
			return defaultChoice.Value, nil
		case "back":
			return "", errSetupBack
		case "exit":
			return "", errSetupExit
		}

		for _, choice := range choices {
			if line == choice.Number || line == choice.Value {
				if choice.Disabled {
					renderSetupErrorLine(out, "%s is not available yet: %s", choice.Label, choice.DisabledReason)
					break
				}
				return choice.Value, nil
			}
		}
		renderSetupErrorLine(out, "Invalid choice. Please enter one of the listed options.")
	}
}

func firstAvailableSetupClientChoice(choices []setupClientChoice) setupClientChoice {
	for _, choice := range choices {
		if !choice.Disabled {
			return choice
		}
	}
	return choices[0]
}

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
	renderSimpleHeader(out, session, "HA NOVA Setup")
}

func clearSetupScreen(out io.Writer) {
	if !resolveSetupUISession(out).clearsScreen() {
		return
	}
	fmt.Fprint(out, "\033[2J\033[H")
}

func renderSetupStep(out io.Writer, step, total int, title string) {
	session := resolveSetupUISession(out)
	stepLabel := fmt.Sprintf("Step %d of %d - %s", step, total, title)
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
	fmt.Fprintf(out, "  %s  %s\n", session.style("accent", bar.String()), session.style("strong", fmt.Sprintf("Step %d of %d - %s", step, total, title)))
}

func renderSetupStatusSummary(out io.Writer, state setupState) {
	session := resolveSetupUISession(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Checking current setup..."))
	if state.RelayOK {
		fmt.Fprintf(out, "  %s Relay reachable\n", session.style("success", session.successMarker()))
	} else {
		fmt.Fprintf(out, "  %s Relay not reachable\n", session.style("error", session.errorMarker()))
	}
	if state.RelayOK {
		fmt.Fprintf(out, "  %s Authentication valid\n", session.style("success", session.successMarker()))
	} else if state.TokenOK {
		fmt.Fprintf(out, "  %s Authentication failed\n", session.style("error", session.errorMarker()))
	} else {
		fmt.Fprintf(out, "  %s No auth token found\n", session.style("error", session.errorMarker()))
	}
	if state.WSOK {
		fmt.Fprintf(out, "  %s WebSocket connected\n", session.style("success", session.successMarker()))
	} else {
		fmt.Fprintf(out, "  %s WebSocket not connected\n", session.style("error", session.errorMarker()))
	}
	if state.SkillsOK {
		fmt.Fprintf(out, "  %s Skills installed\n", session.style("success", session.successMarker()))
	} else {
		fmt.Fprintf(out, "  %s Skills not installed\n", session.style("error", session.errorMarker()))
	}
	fmt.Fprintln(out)
}

func renderSetupDiscoveryResult(out io.Writer, host string, discovered bool) {
	session := resolveSetupUISession(out)
	if discovered {
		fmt.Fprintf(out, "  %s Found Home Assistant candidate: %s\n", session.style("success", session.successMarker()), host)
	} else {
		fmt.Fprintf(out, "  %s No confirmed Home Assistant found automatically; defaulting to homeassistant.local\n", session.style("warning", session.warningMarker()))
	}
	fmt.Fprintln(out)
}

func renderSetupCompleteBanner(out io.Writer, clients []string) {
	session := resolveSetupUISession(out)
	renderSetupHeader(out)
	fmt.Fprintf(out, "  %s Setup complete!\n", session.style("success", session.successMarker()))
	fmt.Fprintln(out)
	if labels := setupClientLabels(clients); labels != "" {
		fmt.Fprintf(out, "  Installed for: %s\n", session.style("strong", labels))
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  Open %s and try asking:\n", session.style("strong", setupClientLabelsForPrompt(clients)))
	fmt.Fprintln(out)
	fmt.Fprintln(out, `    "Turn off the living room light"`)
	fmt.Fprintln(out, `    "List my automations"`)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Need help? Run: ha-nova doctor")
	fmt.Fprintln(out)
}

func renderSetupAlreadyDoneBanner(out io.Writer) {
	session := resolveSetupUISession(out)
	renderSetupHeader(out)
	fmt.Fprintf(out, "  %s Everything is already set up!\n", session.style("success", session.successMarker()))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Run 'ha-nova doctor' for full diagnostics.")
	fmt.Fprintln(out)
}

func renderSetupIncompleteBanner(out io.Writer, issue string) {
	session := resolveSetupUISession(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s Setup incomplete\n", session.style("error", session.errorMarker()))
	fmt.Fprintln(out)
	switch issue {
	case setupIssueWSDegraded:
		fmt.Fprintln(out, "  NOVA Relay is reachable, but Home Assistant WebSocket is not connected yet.")
		fmt.Fprintln(out, "  Open Home Assistant > Settings > Apps > NOVA Relay, verify the app settings, and restart the App.")
	case setupIssueRelayUnreachable:
		fmt.Fprintln(out, "  HA NOVA saved your local setup, but the relay could not be verified yet.")
		fmt.Fprintln(out, "  Open Home Assistant > Settings > Apps > NOVA Relay, start the app, then try again.")
	case setupIssueSkillsInstall:
		fmt.Fprintln(out, "  The Home Assistant connection is configured, but local skill installation still needs another run.")
		fmt.Fprintln(out, "  Re-run setup or install the HA NOVA skills again for your client.")
	default:
		fmt.Fprintln(out, "  HA NOVA saved your local setup, but the system is not fully ready yet.")
	}
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
	case "gemini":
		return "Gemini CLI"
	case "hermes":
		return "Hermes Agent"
	case "all":
		return "your available AI assistants"
	default:
		return "your AI assistant"
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
		return "your AI assistant"
	}
	if len(clients) == 1 {
		return setupClientLabel(clients[0])
	}
	return "your installed AI assistants"
}
