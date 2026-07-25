package main

import (
	"bufio"
	"fmt"
	"io"
)

func handleInteractiveCloudRecoveryBeforeClients(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	serviceMode bool,
	explicitLocalSetup bool,
	lifecycleMarker ...[]byte,
) (runtimeConfig, bool, int) {
	if cfg.Cloud == nil {
		return cfg, false, 0
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(out, cfg, problem)
		return cfg, true, 1
	}
	if !cloudRemoteFeatureAvailable() {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud access is unavailable because this build or platform has Cloud setup disabled.",
		)
		renderCloudCheckpointActions(out, paths, cfg, false)
		return cfg, true, 1
	}
	if remoteOnlyCloudSetup(cfg) {
		if !cfg.Cloud.ready() {
			renderCloudCheckpointActions(out, paths, cfg, true)
			if explicitLocalSetup {
				fmt.Fprintln(
					out,
					"  Finish or remove the saved Home Assistant Cloud checkpoint before changing local or service credentials.",
				)
				return cfg, true, 1
			}
		}
		return cfg, false, 0
	}
	if !hybridCloudSetupPending(cfg) {
		if !cfg.Cloud.ready() {
			renderCloudCheckpointActions(out, paths, cfg, true)
			return cfg, true, 1
		}
		return cfg, false, 0
	}

	updated, handled, code := maybeOfferCloudForCompletedSetup(
		reader,
		out,
		paths,
		cfg,
		serviceMode,
		lifecycleMarker...,
	)
	if !handled {
		renderCloudCheckpointActions(out, paths, cfg, true)
		return cfg, true, 1
	}
	if code != 0 {
		return updated, true, code
	}
	if updated.Cloud != nil && !updated.Cloud.ready() {
		return updated, true, 0
	}
	return updated, false, 0
}
