package main

import "strings"

func joinSetupInputs(parts ...[]string) string {
	lines := []string{}
	for _, part := range parts {
		lines = append(lines, part...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func setupWizardRelayInstallPrompts() []string {
	return []string{
		"",
		"",
	}
}

func setupWizardLLATPrompts() []string {
	return []string{
		"",
		"",
		"",
	}
}

func setupWizardGenerateRelayTokenPrompts() []string {
	return []string{
		"",
		"",
		"",
	}
}

func setupWizardPasteRelayTokenPrompts(token string) []string {
	return []string{
		"1",
		token,
	}
}
