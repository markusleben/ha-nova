package main

import (
	"context"
	"errors"
)

type cloudCommandConnectionTarget struct {
	remote bool
	origin CloudOrigin
}

func resolveCloudCommandConnectionTarget(
	ctx context.Context,
	cfg runtimeConfig,
	options cloudCommandFlags,
	reconnect bool,
) (cloudCommandConnectionTarget, error) {
	remoteURL := cloudRemoteURLForCommand(cfg, options, reconnect)
	if remoteURL == "" &&
		cfg.RelaySecureBaseURL == "" &&
		cfg.RelaySpkiPin == "" {
		origin, err := promptCloudRemoteOriginForCommand(ctx)
		if err != nil {
			if errors.Is(err, errSetupBack) ||
				errors.Is(err, errSetupExit) {
				return cloudCommandConnectionTarget{},
					errCloudURLPromptCancelled
			}
			return cloudCommandConnectionTarget{}, err
		}
		return cloudCommandConnectionTarget{
			remote: true,
			origin: origin,
		}, nil
	}
	if remoteURL == "" {
		return cloudCommandConnectionTarget{}, nil
	}
	origin, err := resolveCanonicalNabuOriginForCloudCommand(
		ctx,
		remoteURL,
		NetCloudCNAMEResolver{},
	)
	if err != nil {
		return cloudCommandConnectionTarget{}, err
	}
	return cloudCommandConnectionTarget{
		remote: true,
		origin: origin,
	}, nil
}
