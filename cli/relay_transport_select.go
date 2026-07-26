package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type relayVia string

const (
	relayViaLocal relayVia = "local"
	relayViaCloud relayVia = "cloud"
)

func parseRelayVia(value string) (relayVia, error) {
	via := relayVia(strings.ToLower(strings.TrimSpace(value)))
	switch via {
	case relayViaLocal, relayViaCloud:
		return via, nil
	default:
		return "", fmt.Errorf("invalid --via value %q: use local or cloud", value)
	}
}

type relayTransportSelection struct {
	BaseURL    string
	Client     *http.Client
	Credential string
	DeviceMode bool
	Via        relayVia
}

type relayTransportResolver func(context.Context, runtimeConfig) (relayTransportSelection, error)

type localRelayPreflightError struct {
	cause error
}

func (e *localRelayPreflightError) Error() string {
	switch {
	case IsCloudErrorCode(e.cause, CloudErrUnauthorized):
		return "local Relay rejected the saved device credential; run: ha-nova setup"
	case IsCloudErrorCode(e.cause, CloudErrForbidden):
		return "local Relay denied the saved device credential; run: ha-nova setup"
	case IsCloudErrorCode(e.cause, CloudErrRedirectRejected):
		return "local Relay redirected an authenticated health check; no Cloud fallback or functional request was attempted"
	case IsCloudErrorCode(e.cause, CloudErrHAProtocol),
		IsCloudErrorCode(e.cause, CloudErrResponseTooLarge):
		return "local Relay returned an unsupported health response; no Cloud fallback or functional request was attempted"
	case errors.Is(e.cause, errPinMismatch):
		return "local Relay TLS identity no longer matches the saved certificate pin; verify the server before pairing again"
	default:
		return "local Relay security preflight failed; no Cloud fallback or functional request was attempted"
	}
}

func (e *localRelayPreflightError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

var resolveLocalRelayTransportForCLI relayTransportResolver = func(_ context.Context, cfg runtimeConfig) (relayTransportSelection, error) {
	base, client, credential, device, err := relayFunctionalTransportForDoctor(cfg)
	return relayTransportSelection{
		BaseURL: base, Client: client, Credential: credential, DeviceMode: device, Via: relayViaLocal,
	}, err
}

// Network/OAuth implementations inject these resolvers. The automatic resolver
// must do only a bounded, authenticated, side-effect-free local preflight. It
// may select Cloud only for a pure network failure; authentication, pin,
// protocol, and version failures must return an error. No resolver may send the
// functional request. Cloud and automatic resolvers must use no-UI native-store
// reads so an ordinary AI/Relay command never opens or waits for an unlock
// prompt. The CLI skeleton stays fail-closed until an adapter is present.
var resolveCloudRelayTransportForCLI relayTransportResolver = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
	return relayTransportSelection{}, cloudAdapterUnavailableProblem()
}

var resolveAutomaticRelayTransportForCLI relayTransportResolver = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
	return relayTransportSelection{}, cloudAdapterUnavailableProblem()
}

func selectRelayTransport(
	ctx context.Context,
	cfg runtimeConfig,
	override relayVia,
	overrideSet bool,
) (relayTransportSelection, error) {
	if overrideSet {
		switch override {
		case relayViaLocal:
			return resolveRelayTransport(ctx, resolveLocalRelayTransportForCLI, cfg)
		case relayViaCloud:
			if err := requireFunctionalCloudAccess(cfg); err != nil {
				return relayTransportSelection{}, err
			}
			return resolveRelayTransport(ctx, resolveCloudRelayTransportForCLI, cfg)
		default:
			return relayTransportSelection{}, fmt.Errorf("unsupported relay transport override %q", override)
		}
	}

	switch effectiveRoutePolicy(cfg.RoutePolicy) {
	case routePolicyLocal:
		return resolveRelayTransport(ctx, resolveLocalRelayTransportForCLI, cfg)
	case routePolicyCloud:
		if err := requireFunctionalCloudAccess(cfg); err != nil {
			return relayTransportSelection{}, err
		}
		return resolveRelayTransport(ctx, resolveCloudRelayTransportForCLI, cfg)
	case routePolicyAutomatic:
		selected, err := resolveRelayTransport(
			ctx,
			resolveAutomaticRelayTransportForCLI,
			cfg,
		)
		if err != nil {
			return relayTransportSelection{}, err
		}
		if selected.Via == relayViaCloud {
			if err := requireFunctionalCloudAccess(cfg); err != nil {
				return relayTransportSelection{}, err
			}
		}
		return selected, nil
	default:
		return relayTransportSelection{}, fmt.Errorf("unsupported route policy %q", cfg.RoutePolicy)
	}
}

func resolveRelayTransport(
	ctx context.Context,
	resolver relayTransportResolver,
	cfg runtimeConfig,
) (relayTransportSelection, error) {
	selected, err := resolver(ctx, cfg)
	if err != nil {
		return relayTransportSelection{}, err
	}
	if selected.Client == nil || strings.TrimSpace(selected.BaseURL) == "" ||
		strings.TrimSpace(selected.Credential) == "" ||
		(selected.Via != relayViaLocal && selected.Via != relayViaCloud) {
		return relayTransportSelection{}, errors.New("relay transport resolver returned an incomplete selection")
	}
	return selected, nil
}

func relayTransportErrorMessage(err error) string {
	var localPreflight *localRelayPreflightError
	if errors.As(err, &localPreflight) {
		return localPreflight.Error()
	}
	var problem *cloudProblem
	if errors.As(err, &problem) {
		return problem.Error()
	}
	var cloudErr *CloudError
	if errors.As(err, &cloudErr) {
		return cloudProblemForError(err).Error()
	}
	return relayAuthTokenProblemMessage(err)
}
