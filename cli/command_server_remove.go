package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	readServerRemoveConfirmationForCommand = readServerRemoveConfirmation
	secretGetForServerRemove               = secretGet
)

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
	removeLifecycleGeneration, lifecycleErr := readInstallLifecycleGeneration(paths)
	if lifecycleErr != nil || censusLifecycleStopped(paths) {
		printHumanErr("HA NOVA install lifecycle is unavailable; run `ha-nova setup` before removing server profiles")
		return 1
	}
	configSnapshot, hadConfigSnapshot, snapshotErr := readOptionalFile(paths.ConfigFile)
	if snapshotErr != nil {
		printHumanErr("cannot inspect the server configuration: %v", snapshotErr)
		return 1
	}
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
	type rawSlotSnapshot struct {
		path    string
		data    []byte
		existed bool
	}
	rawSlotSnapshots := make([]rawSlotSnapshot, 0, len(services))
	backendMarkerPath, markerPathErr := deviceFileBackendMarkerPath()
	if markerPathErr != nil {
		printHumanErr("cannot inspect credential storage mode (%v); nothing was removed", markerPathErr)
		return 1
	}
	backendMarkerData, backendMarkerExists, markerReadErr := readOptionalFile(backendMarkerPath)
	if markerReadErr != nil {
		printHumanErr("cannot inspect credential storage mode (%v); nothing was removed", markerReadErr)
		return 1
	}
	rawSlotSnapshots = append(rawSlotSnapshots, rawSlotSnapshot{
		path:    backendMarkerPath,
		data:    backendMarkerData,
		existed: backendMarkerExists,
	})
	for _, service := range services {
		path, pathErr := deviceSecretFilePath(service)
		if pathErr != nil {
			printHumanErr("cannot inspect stored credentials (%v); nothing was removed", pathErr)
			return 1
		}
		data, existed, readErr := readOptionalFile(path)
		if readErr != nil {
			printHumanErr("cannot inspect stored credentials (%v); nothing was removed", readErr)
			return 1
		}
		rawSlotSnapshots = append(rawSlotSnapshots, rawSlotSnapshot{path: path, data: data, existed: existed})
	}
	credentialValues := make(map[string]string, len(services))
	credentialPresent := make(map[string]bool, len(services))
	credentialSnapshotReliable := true
	for _, service := range services {
		value, readErr := secretGetForServerRemove(service)
		if readErr == nil {
			credentialValues[service] = value
			credentialPresent[service] = true
			continue
		}
		if readErr == errSecretNotFound {
			continue
		}
		// Reachability sentinel + a markerless raw slot file: the purge below
		// deletes raw files directly, and the keyring layer cannot be cleaned
		// from this session either way — warn and proceed (mirrors the rename
		// rule). Without a raw file there is nothing deletable here: abort.
		unreachable := errors.Is(readErr, errDesktopKeyringSessionUnavailable) || errors.Is(readErr, errDesktopKeyringUnavailable)
		if unreachable && !deviceFileBackendMarkerExists() && profileHasRawSlotFile(services) {
			credentialSnapshotReliable = false
			printHumanWarn("secure storage is not reachable here (%v); any keyring credential stored for %q by an earlier desktop pairing is not deleted — clean it from the desktop session if one exists.", readErr, name)
			break
		}
		printHumanErr("secure storage is not reachable here (%v) — removing %q now would leave its stored credential behind. Make secure storage available (e.g. run from the desktop session), then retry; nothing was removed.", readErr, name)
		return 1
	}

	printHumanInfo("Removing server profile %q: its device pairing will be revoked on that relay and its stored credentials deleted. The Relay App on that Home Assistant instance stays installed.", name)
	typed, err := readServerRemoveConfirmationForCommand(name)
	if err != nil {
		printHumanErr("cannot read the confirmation from stdin (%v). Run this command interactively and type the profile name %q to confirm.", err, name)
		return 1
	}
	if typed != name {
		printHumanErr("confirmation %q does not match the profile name %q — nothing was removed.", typed, name)
		return 1
	}
	releaseMutation, mutationOK := acquireServerMutation(paths)
	if !mutationOK {
		return 1
	}
	defer releaseMutation()
	if err := ensureUpdateLifecycleCurrent(paths, removeLifecycleGeneration); err != nil {
		printHumanErr("HA NOVA install lifecycle changed while awaiting confirmation; nothing was removed")
		return 1
	}
	currentDoc, currentOK := loadServerConfigDocument(paths)
	if !currentOK {
		return 1
	}
	if err := ensureOptionalFileSnapshotCurrent(paths.ConfigFile, configSnapshot, hadConfigSnapshot); err != nil {
		printHumanErr("server configuration changed while awaiting confirmation; nothing was removed")
		return 1
	}
	for _, snapshot := range rawSlotSnapshots {
		if err := ensureOptionalFileSnapshotCurrent(snapshot.path, snapshot.data, snapshot.existed); err != nil {
			printHumanErr("stored credentials changed while awaiting confirmation; nothing was removed")
			return 1
		}
	}
	currentDefault := currentDoc.defaultServerName()
	switch {
	case !currentDoc.hasProfile(name):
		printHumanErr("server configuration changed while awaiting confirmation; %q no longer exists", name)
		return 1
	case len(currentDoc.profileNames()) == 1:
		printHumanErr("server configuration changed while awaiting confirmation; %q is now the only profile", name)
		return 1
	case currentDefault == name && !currentDoc.hasProfile(defaultServerProfileName):
		printHumanErr("server configuration changed while awaiting confirmation; set a new default and retry")
		return 1
	}
	doc = currentDoc
	newDefault = currentDefault
	if newDefault == name {
		newDefault = defaultServerProfileName
	}
	for _, service := range services {
		value, readErr := secretGetForServerRemove(service)
		if credentialSnapshotReliable {
			present := readErr == nil
			if (readErr != nil && readErr != errSecretNotFound) ||
				present != credentialPresent[service] ||
				(present && value != credentialValues[service]) {
				printHumanErr("stored credentials changed while awaiting confirmation; nothing was removed")
				return 1
			}
			continue
		}
		unreachable := errors.Is(readErr, errDesktopKeyringSessionUnavailable) || errors.Is(readErr, errDesktopKeyringUnavailable)
		if unreachable && !deviceFileBackendMarkerExists() && profileHasRawSlotFile(services) {
			break
		}
		printHumanErr("secure storage changed while awaiting confirmation; nothing was removed")
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
