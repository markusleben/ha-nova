package main

import (
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
	case "route":
		return runServerRoute(paths, args[1:])
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
	fmt.Fprintln(os.Stdout, "Usage: ha-nova server <list|default|rename|remove|route>")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Subcommands:")
	fmt.Fprintln(os.Stdout, "  list                Show all server profiles (no network calls)")
	fmt.Fprintln(os.Stdout, "  default <name>      Make an existing profile the configured default")
	fmt.Fprintln(os.Stdout, "  rename <old> <new>  Rename a profile and move its credential slots")
	fmt.Fprintln(os.Stdout, "  remove <name>       Revoke and delete a profile (type its name to confirm)")
	fmt.Fprintln(os.Stdout, "  route <policy>      Set local, automatic, or cloud routing")
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
		"Shows every server profile with its HA host, relay URL, route, local",
		"pairing and Cloud state, configured default, and active selection.",
		"No flags, no network calls.") {
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
	fmt.Fprintln(w, "NAME\tHA HOST\tRELAY URL\tROUTE\tPAIRED\tCLOUD\t")
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
		// A working pre-pairing install (default profile, relay URL saved, no
		// pinned secure endpoint) is connected via the shared legacy token and
		// by definition has no meaningful device credential — label the actual
		// state without touching secure storage (headless keyrings would turn
		// this into a misleading "unknown"; issue #419). Named profiles are
		// device-credential-only, so a bare "no" there is accurate.
		paired := "no"
		if cfg, okCfg := doc.flatProfile(name); okCfg && name == defaultServerProfileName && cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != "" {
			paired = "no (legacy token)"
		} else if _, ok, err := readCredentialSlot(deviceCredentialServiceForProfile(name)); err != nil {
			// Slot presence only — a local read, never a relay round-trip.
			paired = "unknown"
		} else if ok {
			paired = "yes"
		}
		var markers []string
		if name == defaultName {
			markers = append(markers, "default")
		}
		if selected != "" && name == selected {
			markers = append(markers, "active ("+selectionSource+")")
		}
		route := routePolicyLocal
		cloud := "no"
		if cfg, ok := doc.flatProfile(name); ok {
			route = effectiveRoutePolicy(cfg.RoutePolicy)
			if cfg.Cloud.ready() && validIdentifier(cfg.RelayInstanceID, 256) {
				cloud = "ready"
			} else if cfg.Cloud.configured() && validIdentifier(cfg.RelayInstanceID, 256) {
				cloud = "updating"
			} else if cfg.Cloud != nil {
				cloud = "pending"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, host, relay, route, paired, cloud, strings.Join(markers, ", "))
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
	releaseMutation, ok := acquireServerMutation(paths)
	if !ok {
		return 1
	}
	defer releaseMutation()
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
	releaseMutation, ok := acquireServerMutation(paths)
	if !ok {
		return 1
	}
	defer releaseMutation()
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
	for _, profile := range []string{oldName, newName} {
		if err := requireSettledDeviceCredentialRetirement(
			paths,
			profile,
		); err != nil {
			printHumanErr("%v. Nothing was renamed.", err)
			return 1
		}
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

func acquireServerMutation(paths runtimePaths) (func(), bool) {
	lifecycleGeneration, err := readInstallLifecycleGeneration(paths)
	if err != nil || censusLifecycleStopped(paths) {
		printHumanErr("HA NOVA install lifecycle is unavailable; run `ha-nova setup` before changing server profiles")
		return func() {}, false
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		printHumanErr("another HA NOVA client update is already in progress")
		return func() {}, false
	}
	if err := ensureUpdateLifecycleCurrent(paths, lifecycleGeneration); err != nil {
		release()
		printHumanErr("%s", err)
		return func() {}, false
	}
	return release, true
}
