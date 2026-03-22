package main

import (
	"fmt"
	"io"
	"time"
)

var detectDefaultHAHostChoiceForSetup = detectDefaultHAHostChoice

func detectDefaultHAHostWithFeedback(out io.Writer, cfg runtimeConfig) (string, bool) {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintf(out, "  Discovering Home Assistant on your network... (up to %ds)\n", int((setupDiscoveryOverallTimeout+time.Second-1)/time.Second))
		fmt.Fprintln(out)
		host, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		renderSetupDiscoveryResult(out, host, discovered)
		return host, discovered
	}

	found, err := runSetupCountdownSpinnerWithResult(out, "Discovering Home Assistant on your network...", setupDiscoveryOverallTimeout, setupDiscoveryMinimumVisibleDuration, func() (setupDiscoveryResult, error) {
		host, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		return setupDiscoveryResult{host: host, discovered: discovered}, nil
	})
	armSetupNextPromptSkipsStaleBlankInput()
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
