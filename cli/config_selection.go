package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Server-profile selection (multi-server support, issue #343). One config file
// holds named server profiles; every command operates on exactly ONE selected
// profile. Selection order: --server flag > HA_NOVA_SERVER env > default_server.
// The selected name also routes the per-profile credential slots through the
// process-global seam below, so the zero-arg slot API stays unchanged.

const defaultServerProfileName = "default"

const serverSelectionEnvVar = "HA_NOVA_SERVER"

var errUnknownServerProfile = errors.New("unknown server profile")

// serverSelectionOverride carries an explicit --server flag value; it wins over
// the environment and the configured default.
var serverSelectionOverride = ""

// activeServerProfileName is the process-global selected-profile seam: resolved
// once when the config is loaded (or by pair's profile creation) and consumed
// by the zero-arg device-credential slot API. It defaults to the default
// profile, so every pre-profile code path keeps today's slot names.
var activeServerProfileName = defaultServerProfileName

func activeServerProfile() string { return activeServerProfileName }

func setActiveServerProfile(name string) {
	if strings.TrimSpace(name) == "" {
		name = defaultServerProfileName
	}
	activeServerProfileName = name
}

func setServerSelectionOverride(name string) { serverSelectionOverride = name }

func serverSelectionFromEnv() string { return strings.TrimSpace(os.Getenv(serverSelectionEnvVar)) }

// requestedServerSelection returns the explicit selection (flag > env) and its
// source label, or "" when the configured default applies.
func requestedServerSelection() (name, source string) {
	if serverSelectionOverride != "" {
		return serverSelectionOverride, "--server"
	}
	if env := serverSelectionFromEnv(); env != "" {
		return env, serverSelectionEnvVar
	}
	return "", ""
}

var serverProfileNamePattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Reserved names would collide with the fixed credential-slot suffixes
// ("ha-nova.device-credential.pending", "….probe").
var reservedServerProfileNames = map[string]bool{"pending": true, "probe": true}

// validateServerProfileName gates profile CREATION. Keyring service strings and
// secret file names inherit the name, so the alphabet stays filesystem- and
// keyring-safe on every platform.
func validateServerProfileName(name string) error {
	if !serverProfileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid server profile name %q: use 1-32 characters of a-z, 0-9, or '-'", name)
	}
	if reservedServerProfileNames[name] {
		return fmt.Errorf("server profile name %q is reserved", name)
	}
	return nil
}

// resolveSelectedServerProfile picks the profile this process operates on.
// An unknown selection fails loud with the list of known profiles: a typo must
// never route a mutation to the wrong house.
func resolveSelectedServerProfile(doc *configDocument) (string, error) {
	names := doc.profileNames()
	pick, source := requestedServerSelection()
	if pick == "" {
		pick = doc.defaultServerName()
		if !doc.hasProfile(pick) {
			return "", fmt.Errorf("%w: default_server %q does not exist in config.json; known server profiles: %s",
				errUnknownServerProfile, pick, strings.Join(names, ", "))
		}
		return pick, nil
	}
	if !doc.hasProfile(pick) {
		return "", fmt.Errorf("%w %q (from %s); known server profiles: %s",
			errUnknownServerProfile, pick, source, strings.Join(names, ", "))
	}
	return pick, nil
}
