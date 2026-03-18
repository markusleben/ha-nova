package main

import (
	"fmt"
	"io"
)

var detectDefaultHAHostChoiceForSetup = detectDefaultHAHostChoice

func detectDefaultHAHostWithFeedback(out io.Writer, cfg runtimeConfig) (string, bool) {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintln(out, "  Discovering Home Assistant on your network...")
		fmt.Fprintln(out)
		host, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		renderSetupDiscoveryResult(out, host, discovered)
		return host, discovered
	}

	found, err := runSetupSpinnerWithResult(out, "Discovering Home Assistant on your network...", setupDiscoveryMinimumVisibleDuration, func() (setupDiscoveryResult, error) {
		host, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		return setupDiscoveryResult{host: host, discovered: discovered}, nil
	})
	if err != nil {
		return "homeassistant.local", false
	}
	renderSetupDiscoveryResult(out, found.host, found.discovered)
	return found.host, found.discovered
}

type setupDiscoveryResult struct {
	host       string
	discovered bool
}
