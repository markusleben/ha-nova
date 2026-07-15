package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var discoverReachableHAHostsForSetup = discoverReachableHAHosts

type setupDiscoveryResult struct {
	candidates []setupDiscoveryCandidate
	fallback   string
}

func selectDefaultHAHostWithFeedback(reader *bufio.Reader, out io.Writer, cfg runtimeConfig) (setupDiscoveryCandidate, bool, error) {
	result := runSetupDiscoveryWithFeedback(out, cfg)
	switch len(result.candidates) {
	case 0:
		renderSetupDiscoveryResult(out, result.fallback, "", false)
		return setupDiscoveryCandidate{Host: result.fallback}, false, nil
	case 1:
		candidate := result.candidates[0]
		renderSetupDiscoveryResult(out, candidate.Host, candidate.Via, true)
		needsAddressConfirmation := isLocalDiscoveryHost(candidate.Host)
		return candidate, !needsAddressConfirmation, nil
	default:
		fmt.Fprintf(out, "  Found %d reachable Home Assistant instances. Choose the one to set up.\n", len(result.candidates))
		return promptSetupDiscoveryCandidateInteractive(reader, out, result.candidates)
	}
}

func runSetupDiscoveryWithFeedback(out io.Writer, cfg runtimeConfig) setupDiscoveryResult {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  Discovering Home Assistant on your network... (up to %ds)\n", int((setupDiscoveryOverallTimeout+time.Second-1)/time.Second))
		fmt.Fprintln(out)
		candidates, fallback := discoverReachableHAHostsForSetup(cfg)
		return setupDiscoveryResult{candidates: candidates, fallback: fallback}
	}

	found, err := runSetupCountdownSpinnerWithResult(out, "Discovering Home Assistant on your network...", setupDiscoveryOverallTimeout, setupDiscoveryMinimumVisibleDuration, func() (setupDiscoveryResult, error) {
		candidates, fallback := discoverReachableHAHostsForSetup(cfg)
		return setupDiscoveryResult{candidates: candidates, fallback: fallback}, nil
	})
	armSetupNextPromptSkipsStaleBlankInput()
	if err != nil {
		return setupDiscoveryResult{fallback: preferredUnverifiedHAHost(cfg)}
	}
	return found
}

func promptSetupDiscoveryCandidateInteractive(reader *bufio.Reader, out io.Writer, candidates []setupDiscoveryCandidate) (setupDiscoveryCandidate, bool, error) {
	spec := setupMenuSpec{
		Title:        "Choose your Home Assistant:",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "0",
	}
	for idx, candidate := range candidates {
		spec.Options = append(spec.Options, setupMenuOption{
			Value: strconv.Itoa(idx),
			Label: setupDiscoveryCandidateLabel(candidate),
		})
	}
	spec.Options = append(spec.Options, setupMenuOption{Value: "manual", Label: "Enter a different address"})

	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		return promptSetupDiscoveryCandidateFromReader(reader, out, candidates)
	})
	if err != nil {
		return setupDiscoveryCandidate{}, false, err
	}
	if answer == "manual" {
		return setupDiscoveryCandidate{}, false, nil
	}
	idx, err := strconv.Atoi(answer)
	if err != nil || idx < 0 || idx >= len(candidates) {
		return setupDiscoveryCandidate{}, false, fmt.Errorf("invalid Home Assistant discovery choice")
	}
	return candidates[idx], true, nil
}

func promptSetupDiscoveryCandidateFromReader(reader *bufio.Reader, out io.Writer, candidates []setupDiscoveryCandidate) (string, error) {
	staleBlankDeadline := beginSetupStaleBlankInputWindow()
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  Choose your Home Assistant:")
		fmt.Fprintln(out)
		for idx, candidate := range candidates {
			fmt.Fprintf(out, "    %d) %s\n", idx+1, setupDiscoveryCandidateLabel(candidate))
		}
		manualNumber := len(candidates) + 1
		fmt.Fprintf(out, "    %d) Enter a different address\n", manualNumber)
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  Enter [1-%d] (default 1, or type 'back'/'exit'): ", manualNumber)

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if setupInputIsStaleBlank(staleBlankDeadline, line == "") {
			rerenderSetupPromptAfterStaleBlank(out)
			continue
		}
		staleBlankDeadline = time.Time{}
		switch line {
		case "":
			return "0", nil
		case "back":
			return "", errSetupBack
		case "exit":
			return "", errSetupExit
		case "manual":
			return "manual", nil
		}

		choice, err := strconv.Atoi(line)
		switch {
		case err != nil || choice < 1 || choice > manualNumber:
			renderSetupErrorLine(out, "Invalid choice. Please enter one of the listed options.")
		case choice == manualNumber:
			return "manual", nil
		default:
			return strconv.Itoa(choice - 1), nil
		}
	}
}

func isLocalDiscoveryHost(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(normalizeHostInput(host)), ".")
	return strings.HasSuffix(normalized, ".local")
}

func setupDiscoveryCandidateLabel(candidate setupDiscoveryCandidate) string {
	source := candidate.Source
	if candidate.Via != "" {
		source += " via " + candidate.Via
	}
	return fmt.Sprintf("%s (%s)", candidate.Host, source)
}
