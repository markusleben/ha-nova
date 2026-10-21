package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// handleInteractiveCloudRecoveryBeforeClients settles a saved Home Assistant
// Link checkpoint before setup asks for clients. paused: a plain interactive
// setup (no flags, no --service) goes on with Link left as it is and changes
// no credentials (setup_flow_link_paused.go); a safety hold, a build without
// Link, flags, --service, and Exit still end setup here.
func handleInteractiveCloudRecoveryBeforeClients(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	serviceMode bool,
	explicitLocalSetup bool,
	lifecycleMarker ...[]byte,
) (updated runtimeConfig, handled bool, code int, paused bool) {
	if cfg.Cloud == nil {
		return cfg, false, 0, false
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(out, cfg, problem)
		return cfg, true, 1, false
	}
	if !cloudRemoteFeatureAvailable() {
		fmt.Fprintln(
			out,
			"  Home Assistant Link access is unavailable because this build or platform has Cloud setup disabled.",
		)
		renderCloudCheckpointActions(out, paths, cfg, false)
		return cfg, true, 1, false
	}
	plain := !serviceMode && !explicitLocalSetup
	if remoteOnlyCloudSetup(cfg) {
		if !cfg.Cloud.ready() {
			renderCloudCheckpointActions(out, paths, cfg, true)
			if explicitLocalSetup {
				fmt.Fprintln(
					out,
					"  Finish or remove the saved Home Assistant Link checkpoint before changing local or service credentials.",
				)
				return cfg, true, 1, false
			}
		}
		return cfg, false, 0, false
	}
	if !hybridCloudSetupPending(cfg) {
		if !cfg.Cloud.ready() {
			renderCloudCheckpointActions(out, paths, cfg, true)
			if plain {
				return cfg, false, 0, true
			}
			return cfg, true, 1, false
		}
		return cfg, false, 0, false
	}

	exited := false
	ask := func(prompt, yesLabel string, defaultYes bool) (bool, error) {
		yes, err := promptWizardYesNoMenu(reader, out, prompt, yesLabel, "Not now", defaultYes)
		exited = errors.Is(err, errSetupExit)
		return yes, err
	}
	updated, handled, code = offerCloudForCompletedSetup(
		ask,
		out,
		paths,
		cfg,
		serviceMode,
		lifecycleMarker...,
	)
	if !handled {
		renderCloudCheckpointActions(out, paths, cfg, true)
		return cfg, true, 1, false
	}
	if code != 0 {
		return updated, true, code, false
	}
	if updated.Cloud != nil && !updated.Cloud.ready() {
		// Not now, or a reconnect owns the state: its commands are shown.
		if plain && !exited {
			return updated, false, 0, true
		}
		return updated, true, 0, false
	}
	return updated, false, 0, false
}
