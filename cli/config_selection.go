package main

import (
	"errors"
	"flag"
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
var errInvalidServerProfileSelection = errors.New(
	"invalid server profile selection",
)

// serverSelectionOverride carries an explicit --server flag value; it wins over
// the environment and the configured default.
var serverSelectionOverride = ""

// activeServerProfileName is the process-global selected-profile seam: resolved
// once when the config is loaded (or by pair's profile creation) and consumed
// by the zero-arg device-credential slot API. It defaults to the default
// profile, so every pre-profile code path keeps today's slot names.
var activeServerProfileName = defaultServerProfileName

// activeDemo: the selected profile is the demo house from demo.json (`ha-nova
// try`), not a real server that happens to be called "demo".
var activeDemo = false

func activeServerProfile() string { return activeServerProfileName }

func activeServerIsDemo() bool { return activeDemo }

func setActiveServerProfile(name string) {
	if strings.TrimSpace(name) == "" {
		name = defaultServerProfileName
	}
	activeServerProfileName = name
	activeDemo = false
}

func setActiveDemoProfile() {
	activeServerProfileName = demoProfileName
	activeDemo = true
}

func setServerSelectionOverride(name string) { serverSelectionOverride = name }

var errEmptyServerFlag = errors.New("--server requires a non-empty profile name")

// selectServerFlag applies a parsed --server flag of a FlagSet command.
func selectServerFlag(fs *flag.FlagSet, server string) error {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == "server" })
	if !set {
		return nil
	}
	if strings.TrimSpace(server) == "" {
		return errEmptyServerFlag
	}
	setServerSelectionOverride(server)
	return nil
}

// takeServerArg does the same for hand-parsed commands (trace): it removes
// --server <name> / --server=<name> from args and selects that profile.
func takeServerArg(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if name != "--server" && name != "-server" {
			rest = append(rest, args[i])
			continue
		}
		if !inline && i+1 < len(args) {
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return nil, errEmptyServerFlag
		}
		setServerSelectionOverride(value)
	}
	return rest, nil
}

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
		if err := validateServerProfileName(pick); err != nil {
			return "", fmt.Errorf(
				"%w: invalid selected server profile from default_server: %w",
				errInvalidServerProfileSelection,
				err,
			)
		}
		if !doc.hasProfile(pick) {
			return "", fmt.Errorf("%w: default_server %q does not exist in config.json; known server profiles: %s",
				errUnknownServerProfile, pick, strings.Join(names, ", "))
		}
		return pick, nil
	}
	if err := validateServerProfileName(pick); err != nil {
		return "", fmt.Errorf(
			"%w: invalid selected server profile from %s: %w",
			errInvalidServerProfileSelection,
			source,
			err,
		)
	}
	if !doc.hasProfile(pick) {
		return "", fmt.Errorf("%w %q (from %s); known server profiles: %s",
			errUnknownServerProfile, pick, source, strings.Join(names, ", "))
	}
	return pick, nil
}
