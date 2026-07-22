package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// `ha-nova server` — server-profile administration for multi-server support
// (issue #343). The wizard onboards new servers and `pair --server` creates
// profiles; THIS command family administers them: list, switch the configured
// default, rename, remove. The LITERAL default profile owns the legacy-token
// machinery and the downgrade mirror for older binaries, so it can be neither
// renamed nor removed here — `ha-nova uninstall` handles the last server.

func runServerCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printServerUsage()
		return 1
	}
	switch args[0] {
	case "list":
		return runServerList(paths, args[1:])
	case "default":
		return runServerDefault(paths, args[1:])
	case "rename":
		return runServerRename(paths, args[1:])
	case "remove":
		return runServerRemove(paths, args[1:])
	case "-h", "--help", "help":
		printServerUsage()
		return 0
	default:
		printHumanErr("unknown server subcommand: %s", args[0])
		printServerUsage()
		return 1
	}
}

func printServerUsage() {
	fmt.Fprintln(os.Stdout, "Usage: ha-nova server <list|default|rename|remove>")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Subcommands:")
	fmt.Fprintln(os.Stdout, "  list                Show all server profiles (no network calls)")
	fmt.Fprintln(os.Stdout, "  default <name>      Make an existing profile the configured default")
	fmt.Fprintln(os.Stdout, "  rename <old> <new>  Rename a profile and move its credential slots")
	fmt.Fprintln(os.Stdout, "  remove <name>       Revoke and delete a profile (type its name to confirm)")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Add a new server with: ha-nova pair --server <name> --relay-url http://<ha-host>:8791")
	fmt.Fprintln(os.Stdout, "Run 'ha-nova server <subcommand> --help' for details.")
}

// serverSubcommandHelp answers -h/--help for a flagless server subcommand:
// usage plus description lines on stdout, reported back so callers exit 0.
func serverSubcommandHelp(args []string, usage string, description ...string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprintln(os.Stdout, "Usage: "+usage)
			for _, line := range description {
				fmt.Fprintln(os.Stdout, line)
			}
			return true
		}
	}
	return false
}

func loadServerConfigDocument(paths runtimePaths) (*configDocument, bool) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot read the server configuration (%s): %v. Run: ha-nova setup", paths.ConfigFile, err)
		return nil, false
	}
	return doc, true
}

func unknownServerProfileError(doc *configDocument, name string) {
	printHumanErr("unknown server profile %q; known server profiles: %s. Add a new server with: ha-nova pair --server <name> --relay-url http://<ha-host>:8791",
		name, strings.Join(doc.profileNames(), ", "))
}

func runServerList(paths runtimePaths, args []string) int {
	if serverSubcommandHelp(args, "ha-nova server list",
		"Shows every server profile with its HA host, relay URL, pairing state,",
		"the configured default, and the active selection. No flags, no network calls.") {
		return 0
	}
	if len(args) > 0 {
		printHumanErr("server list takes no arguments")
		return 1
	}
	doc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	defaultName := doc.defaultServerName()
	selected, selectionSource := requestedServerSelection()

	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHA HOST\tRELAY URL\tPAIRED\t")
	for _, name := range doc.profileNames() {
		host, relay := "-", "-"
		if cfg, ok := doc.flatProfile(name); ok {
			if strings.TrimSpace(cfg.HAHost) != "" {
				host = cfg.HAHost
			}
			if strings.TrimSpace(cfg.RelayBaseURL) != "" {
				relay = cfg.RelayBaseURL
			}
		}
		// Slot presence only — a local read, never a relay round-trip.
		paired := "no"
		if _, ok, err := readCredentialSlot(deviceCredentialServiceForProfile(name)); err != nil {
			paired = "unknown"
		} else if ok {
			paired = "yes"
		} else if cfg, okCfg := doc.flatProfile(name); okCfg && cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != "" {
			// A working pre-pairing install: connected via the shared legacy
			// token, no per-device credential yet. Plain "no" reads as broken
			// (issue #419) — label the actual state.
			paired = "no (legacy token)"
		}
		var markers []string
		if name == defaultName {
			markers = append(markers, "default")
		}
		if selected != "" && name == selected {
			markers = append(markers, "active ("+selectionSource+")")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, host, relay, paired, strings.Join(markers, ", "))
	}
	w.Flush()
	return 0
}

func runServerDefault(paths runtimePaths, args []string) int {
	if serverSubcommandHelp(args, "ha-nova server default <name>",
		"Sets default_server to an existing profile. Runtime selection stays",
		"--server flag > HA_NOVA_SERVER env > this configured default.") {
		return 0
	}
	if len(args) != 1 {
		printHumanErr("Usage: ha-nova server default <name>")
		return 1
	}
	name := args[0]
	doc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	if !doc.hasProfile(name) {
		unknownServerProfileError(doc, name)
		return 1
	}
	servers, err := documentServersCopy(doc)
	if err != nil {
		printHumanErr("cannot update the server configuration: %v", err)
		return 1
	}
	if err := writeServersDocument(paths, doc, servers, name); err != nil {
		printHumanErr("cannot save the server configuration: %v", err)
		return 1
	}
	printHumanInfo("Default server is now %q.", name)
	return 0
}

func runServerRename(paths runtimePaths, args []string) int {
	if serverSubcommandHelp(args, "ha-nova server rename <old> <new>",
		"Renames a server profile and moves its device-credential slots to the",
		"new name. The \"default\" profile cannot be renamed.") {
		return 0
	}
	if len(args) != 2 {
		printHumanErr("Usage: ha-nova server rename <old> <new>")
		return 1
	}
	oldName, newName := args[0], args[1]
	doc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	if oldName == defaultServerProfileName {
		printHumanErr("the %q profile cannot be renamed: it owns the legacy relay token and the downgrade mirror for older binaries. Add the server under a new name instead: ha-nova pair --server <name> --relay-url http://<ha-host>:8791",
			defaultServerProfileName)
		return 1
	}
	if !doc.hasProfile(oldName) {
		unknownServerProfileError(doc, oldName)
		return 1
	}
	if newName == defaultServerProfileName {
		printHumanErr("renaming to %q is not allowed: that name is reserved for the legacy-token profile", defaultServerProfileName)
		return 1
	}
	if err := validateServerProfileName(newName); err != nil {
		printHumanErr("%v", err)
		return 1
	}
	if doc.hasProfile(newName) {
		printHumanErr("server profile %q already exists; pick another name or remove it first: ha-nova server remove %s", newName, newName)
		return 1
	}

	// Copy the credential slots to the new services BEFORE touching the config:
	// a failure here leaves everything under the old name.
	rollbackSlots, deleteOldSlots, err := stageServerCredentialSlotMove(oldName, newName)
	if err != nil {
		printHumanErr("%v", err)
		return 1
	}

	servers, err := documentServersCopy(doc)
	if err != nil {
		rollbackSlots()
		printHumanErr("cannot update the server configuration: %v", err)
		return 1
	}
	servers[newName] = servers[oldName]
	delete(servers, oldName)
	defaultName := doc.defaultServerName()
	if defaultName == oldName {
		defaultName = newName
	}
	if err := writeServersDocument(paths, doc, servers, defaultName); err != nil {
		rollbackSlots()
		printHumanErr("cannot save the server configuration: %v — the rename was not applied", err)
		return 1
	}
	deleteOldSlots()
	printHumanInfo("Renamed server profile %q to %q.", oldName, newName)
	if defaultName == newName {
		printHumanInfo("default_server now points at %q.", newName)
	}
	return 0
}

// stageServerCredentialSlotMove copies both credential slots (current +
// pending) of oldName to newName's services and returns a rollback (drop the
// copies) and a commit (delete the old slots, best-effort). Read/write errors
// fail the whole rename — the credentials must travel with the name.
func runServerRemove(paths runtimePaths, args []string) int {
	if serverSubcommandHelp(args, "ha-nova server remove <name>",
		"Revokes the profile's device pairing on ITS relay (best-effort), deletes",
		"its stored credentials, and removes the profile from config.json.",
		"Asks you to type the profile name to confirm — there is no bypass flag.") {
		return 0
	}
	if len(args) != 1 {
		printHumanErr("Usage: ha-nova server remove <name>")
		return 1
	}
	name := args[0]
	doc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	if !doc.hasProfile(name) {
		unknownServerProfileError(doc, name)
		return 1
	}
	if len(doc.profileNames()) == 1 {
		// Holds for the literal default AND for a multi-server-first install
		// whose only profile is a named one (pair --server first).
		printHumanErr("%q is the only server profile — to remove HA NOVA from this machine, run: ha-nova uninstall", name)
		return 1
	}
	if name == defaultServerProfileName {
		printHumanErr("the %q profile cannot be removed while other server profiles exist: it owns the legacy relay token and the downgrade mirror. Remove the other profiles first, or remove everything with: ha-nova uninstall",
			defaultServerProfileName)
		return 1
	}
	newDefault := doc.defaultServerName()
	if newDefault == name {
		if !doc.hasProfile(defaultServerProfileName) {
			var others []string
			for _, profile := range doc.profileNames() {
				if profile != name {
					others = append(others, profile)
				}
			}
			printHumanErr("%q is the configured default server and no %q profile exists to fall back to. Set a new default first: ha-nova server default <name> (available: %s)",
				name, defaultServerProfileName, strings.Join(others, ", "))
			return 1
		}
		newDefault = defaultServerProfileName
	}

	// Secure-storage preflight BEFORE the confirmation: if the slots are not
	// reachable here (locked keyring, headless SSH), removing the config entry
	// would strand credentials under a name that no longer exists. Abort with
	// nothing touched instead. Reachability only — a MALFORMED stored value is
	// no reason to refuse: the purge deletes it without parsing.
	services := []string{deviceCredentialServiceForProfile(name), deviceCredentialPendingServiceForProfile(name)}
	for _, service := range services {
		_, readErr := secretGet(service)
		if readErr == nil || readErr == errSecretNotFound {
			continue
		}
		// Reachability sentinel + a markerless raw slot file: the purge below
		// deletes raw files directly, and the keyring layer cannot be cleaned
		// from this session either way — warn and proceed (mirrors the rename
		// rule). Without a raw file there is nothing deletable here: abort.
		unreachable := errors.Is(readErr, errDesktopKeyringSessionUnavailable) || errors.Is(readErr, errDesktopKeyringUnavailable)
		if unreachable && !deviceFileBackendMarkerExists() && profileHasRawSlotFile(services) {
			printHumanWarn("secure storage is not reachable here (%v); any keyring credential stored for %q by an earlier desktop pairing is not deleted — clean it from the desktop session if one exists.", readErr, name)
			break
		}
		printHumanErr("secure storage is not reachable here (%v) — removing %q now would leave its stored credential behind. Make secure storage available (e.g. run from the desktop session), then retry; nothing was removed.", readErr, name)
		return 1
	}

	printHumanInfo("Removing server profile %q: its device pairing will be revoked on that relay and its stored credentials deleted. The Relay App on that Home Assistant instance stays installed.", name)
	typed, err := readServerRemoveConfirmation(name)
	if err != nil {
		printHumanErr("cannot read the confirmation from stdin (%v). Run this command interactively and type the profile name %q to confirm.", err, name)
		return 1
	}
	if typed != name {
		printHumanErr("confirmation %q does not match the profile name %q — nothing was removed.", typed, name)
		return 1
	}

	// Config first: a failed save aborts cleanly with the pairing untouched.
	// The endpoint for the revoke is captured from the in-memory document, so
	// removing the entry first loses nothing. If the revoke afterwards fails,
	// the report says so and the device can still be revoked from the NOVA
	// console — the softer failure than a configured-but-unpaired profile.
	cfg, _ := doc.flatProfile(name)
	servers, err := documentServersCopy(doc)
	if err != nil {
		printHumanErr("cannot update the server configuration: %v — nothing was removed.", err)
		return 1
	}
	delete(servers, name)
	if err := writeServersDocument(paths, doc, servers, newDefault); err != nil {
		printHumanErr("cannot save the server configuration: %v — nothing was removed; fix the error and run the remove again.", err)
		return 1
	}

	// Now revoke against THIS profile's pinned endpoint and drop both slots.
	report := &uninstallReport{}
	purgeProfileDeviceCredentialWithReport(profilePurgeTarget{
		name:          name,
		secureBaseURL: strings.TrimSpace(cfg.RelaySecureBaseURL),
		spkiPin:       strings.TrimSpace(cfg.RelaySpkiPin),
	}, report, false)
	report.printDetails()
	printHumanInfo("Removed server profile %q.", name)
	if newDefault != doc.defaultServerName() {
		printHumanInfo("default_server was reset to %q.", newDefault)
	}
	return 0
}

// readServerRemoveConfirmation prompts on stdout and reads one line from
// stdin. The typed profile name IS the confirmation — there is no bypass
// flag. A closed/non-interactive stdin fails loud instead of removing.
func readServerRemoveConfirmation(name string) (string, error) {
	fmt.Fprintf(os.Stdout, "Type the profile name %q to confirm removal: ", name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	typed := strings.TrimSpace(line)
	if err != nil && typed == "" {
		return "", err
	}
	return typed, nil
}
