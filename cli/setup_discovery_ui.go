package main

import (
	"fmt"
	"io"
	"time"
)

var detectDefaultHAHostChoiceForSetup = detectDefaultHAHostChoice

func detectDefaultHAHostWithFeedback(out io.Writer, cfg runtimeConfig) (string, bool) {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  Discovering Home Assistant on your network... (up to %ds)\n", int((setupDiscoveryOverallTimeout+time.Second-1)/time.Second))
		fmt.Fprintln(out)
		host, via, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		renderSetupDiscoveryResult(out, host, via, discovered)
		return host, discovered
	}

	found, err := runSetupCountdownSpinnerWithResult(out, "Discovering Home Assistant on your network...", setupDiscoveryOverallTimeout, setupDiscoveryMinimumVisibleDuration, func() (setupDiscoveryResult, error) {
		host, via, discovered := detectDefaultHAHostChoiceForSetup(cfg)
		return setupDiscoveryResult{host: host, via: via, discovered: discovered}, nil
	})
	armSetupNextPromptSkipsStaleBlankInput()
	if err != nil {
		return preferredUnverifiedHAHost(cfg), false
	}
	renderSetupDiscoveryResult(out, found.host, found.via, found.discovered)
	return found.host, found.discovered
}

type setupDiscoveryResult struct {
	host       string
	via        string
	discovered bool
}
