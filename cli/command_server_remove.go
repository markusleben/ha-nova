package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var (
	readServerRemoveConfirmationForCommand = readServerRemoveConfirmation
	secretGetForServerRemove               = secretGet
	serverRemovalPhaseHook                 = func(string) error { return nil }
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
	if cfg, exists := doc.flatProfile(name); exists &&
		cfg.ServerRemoval != nil {
		releaseMutation, mutationOK := acquireServerMutation(paths)
		if !mutationOK {
			return 1
		}
		defer releaseMutation()
		currentDoc, currentOK := loadServerConfigDocument(paths)
		if !currentOK {
			return 1
		}
		return completeServerRemovalUnlocked(
			paths,
			currentDoc,
			name,
		)
	}
	retirementPending, retirementErr :=
		deviceCredentialRetirementCheckpointExistsForProfile(paths, name)
	if retirementErr != nil || retirementPending {
		if retirementErr != nil {
			printHumanErr("cannot inspect pending device retirement for server profile %q (%v); nothing was removed", name, retirementErr)
		} else {
			printHumanErr("server profile %q has a pending device retirement; run `%s` to finish it before removing the profile. Nothing was removed.", name, deviceRetirementSetupCommand(name))
		}
		return 1
	}
	hasCloudState, cloudStateErr := serverProfileContainsCloudState(doc, name)
	if cloudStateErr != nil {
		printHumanErr("cannot safely inspect Home Assistant Cloud state for server profile %q (%v); nothing was removed", name, cloudStateErr)
		return 1
	}
	if hasCloudState {
		if len(doc.profileNames()) == 1 {
			printHumanErr("%q is the only server profile and still has Home Assistant Cloud state. Revoke its authorization and remove HA NOVA with: ha-nova uninstall --purge", name)
			return 1
		}
		printHumanErr("server profile %q still has Home Assistant Cloud state; remove Cloud access first so its native secure-storage credentials are not stranded. Nothing was removed.", name)
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
	retirementPending, retirementErr =
		deviceCredentialRetirementCheckpointExistsForProfile(paths, name)
	if retirementErr != nil || retirementPending {
		printHumanErr("pending device retirement changed while awaiting confirmation; nothing was removed")
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
		printHumanErr("secure storage changed while awaiting confirmation; nothing was removed")
		return 1
	}
	cfg, _ := doc.flatProfile(name)
	profileRaw, rawErr := cloudRecoveryProfileRaw(doc, name)
	if rawErr != nil {
		printHumanErr(
			"cannot preserve the server profile for safe removal: %v; nothing was removed",
			rawErr,
		)
		return 1
	}
	checkpoint := newServerRemovalCheckpoint(
		name,
		cfg,
		profileRaw,
		credentialValues[services[0]],
		credentialValues[services[1]],
	)
	if err := writeServerRemovalCheckpoint(
		paths,
		doc,
		name,
		checkpoint,
	); err != nil {
		printHumanErr(
			"cannot save the durable server-removal checkpoint: %v — nothing was removed",
			err,
		)
		return 1
	}
	if err := serverRemovalPhaseHook("checkpoint-persisted"); err != nil {
		printHumanErr(
			"server removal paused after its durable checkpoint: %v. Run the same command again to resume safely.",
			err,
		)
		return 1
	}
	checkpointedDoc, checkpointedOK :=
		loadServerConfigDocument(paths)
	if !checkpointedOK {
		return 1
	}
	return completeServerRemovalUnlocked(
		paths,
		checkpointedDoc,
		name,
	)
}

func serverProfileContainsCloudState(doc *configDocument, name string) (bool, error) {
	if doc.servers == nil {
		raw, exists := doc.top["cloud"]
		return exists && strings.TrimSpace(string(raw)) != "null", nil
	}
	raw, exists := doc.servers[name]
	if !exists {
		return false, nil
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil {
		return false, err
	}
	if profile == nil {
		return false, fmt.Errorf("server profile is null")
	}
	cloud, exists := profile["cloud"]
	return exists && strings.TrimSpace(string(cloud)) != "null", nil
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
