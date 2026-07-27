package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type serverRouteOptions struct {
	Policy    routePolicy
	Server    string
	ServerSet bool
}

func parseServerRouteOptions(args []string) (serverRouteOptions, error) {
	fs := flag.NewFlagSet("server route", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var server string
	fs.StringVar(&server, "server", "", "server profile name")
	if len(args) == 0 {
		return serverRouteOptions{}, errors.New("Usage: ha-nova server route <local|automatic|cloud> [--server <name>]")
	}
	if args[0] == "--help" || args[0] == "-h" {
		if err := fs.Parse(args); err != nil {
			if helpRequested(err, fs, "ha-nova server route <local|automatic|cloud> [--server <name>]") {
				return serverRouteOptions{}, errHelpShown
			}
			return serverRouteOptions{}, err
		}
	}
	policy, err := parseRoutePolicy(args[0])
	if err != nil {
		return serverRouteOptions{}, err
	}
	if err := fs.Parse(args[1:]); err != nil {
		if helpRequested(err, fs, "ha-nova server route <local|automatic|cloud> [--server <name>]") {
			return serverRouteOptions{}, errHelpShown
		}
		return serverRouteOptions{}, err
	}
	if fs.NArg() != 0 {
		return serverRouteOptions{}, errors.New("Usage: ha-nova server route <local|automatic|cloud> [--server <name>]")
	}
	serverSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "server" {
			serverSet = true
		}
	})
	if serverSet && strings.TrimSpace(server) == "" {
		return serverRouteOptions{}, errors.New("--server requires a non-empty profile name")
	}
	return serverRouteOptions{Policy: policy, Server: server, ServerSet: serverSet}, nil
}

func runServerRoute(paths runtimePaths, args []string) int {
	opts, err := parseServerRouteOptions(args)
	if err != nil {
		if errors.Is(err, errHelpShown) {
			return 0
		}
		printHumanErr("%v", err)
		return 1
	}
	if opts.ServerSet {
		setServerSelectionOverride(opts.Server)
	}
	releaseMutation, ok := acquireServerMutation(paths)
	if !ok {
		return 1
	}
	defer releaseMutation()

	doc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		printHumanErr("%v", err)
		return 1
	}
	cfg, ok := doc.flatProfile(name)
	if !ok {
		unknownServerProfileError(doc, name)
		return 1
	}
	if err := rejectPendingServerRemoval(name, cfg); err != nil {
		printHumanErr("%v", err)
		return 1
	}
	if opts.Policy != routePolicyLocal {
		if err := requireCloudRemoteFeature(); err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}
	if opts.Policy != routePolicyLocal && !cfg.Cloud.configured() {
		printHumanErr("server profile %q has no completed Home Assistant Cloud setup; add away-from-home access before selecting %s routing", name, opts.Policy)
		return 1
	}
	if opts.Policy == routePolicyAutomatic &&
		(strings.TrimSpace(cfg.RelaySecureBaseURL) == "" || strings.TrimSpace(cfg.RelaySpkiPin) == "") {
		printHumanErr("server profile %q has no completed local device pairing; automatic routing requires both local and Cloud transports", name)
		return 1
	}
	cfg.RoutePolicy = opts.Policy
	if err := saveConfig(paths, cfg); err != nil {
		printHumanErr("cannot save route policy for server profile %q: %v", name, err)
		return 1
	}
	printHumanInfo("Server profile %q now uses %s routing.", name, opts.Policy)
	if opts.Policy == routePolicyAutomatic {
		fmt.Println("Local is preferred; Cloud is used only after a safe preflight selects it.")
	} else if opts.Policy == routePolicyLocal &&
		strings.TrimSpace(cfg.RelayBaseURL) == "" {
		fmt.Println("Cloud routing is disabled. This profile has no local Relay connection and will stay offline until local access is configured.")
	}
	return 0
}
