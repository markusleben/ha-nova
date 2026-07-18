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

func setupWizardGenerateRelayTokenPrompts() []string {
	return []string{
		"",
		"",
		"",
		"",
		"manual",
		"",
		"",
		"",
	}
}

func setupWizardPasteRelayTokenPrompts(token string) []string {
	return []string{
		"",
		"",
		"",
		"",
		"manual",
		"1",
		token,
	}
}

func setupWizardPairingPrompts(code string) []string {
	return []string{
		"",
		"",
		"",
		"",
		code,
	}
}
