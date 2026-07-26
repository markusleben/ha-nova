package main

func completeServerRemovalUnlocked(
	paths runtimePaths,
	doc *configDocument,
	name string,
) int {
	cfg, exists := doc.flatProfile(name)
	if !exists {
		printHumanErr(
			"server removal checkpoint lost profile %q; no credentials were touched",
			name,
		)
		return 1
	}
	if err := validateServerRemovalCheckpointDocument(
		doc,
		name,
		cfg,
	); err != nil {
		printHumanErr(
			"cannot resume server removal safely: %v",
			err,
		)
		return 1
	}
	if hasCloud, err :=
		serverProfileContainsCloudState(doc, name); err != nil ||
		hasCloud {
		printHumanErr(
			"server profile %q changed during removal and now contains Home Assistant Cloud state; preserved it for explicit Cloud cleanup",
			name,
		)
		return 1
	}
	purgeTarget, err := profilePurgeTargetFromConfig(name, cfg)
	if err != nil {
		printHumanErr(
			"cannot validate server credential cleanup: %v",
			err,
		)
		return 1
	}
	report := &uninstallReport{}
	if err := purgeProfileDeviceCredentialWithReport(
		purgeTarget,
		report,
		false,
	); err != nil {
		report.printDetails()
		printHumanErr(
			"server credential cleanup paused: %v; its durable profile checkpoint remains for a safe retry",
			err,
		)
		return 1
	}
	if err := serverRemovalPhaseHook("credentials-purged"); err != nil {
		report.printDetails()
		printHumanErr(
			"server removal paused after credential cleanup: %v. Its profile remains checkpointed for a safe retry.",
			err,
		)
		return 1
	}
	currentDoc, ok := loadServerConfigDocument(paths)
	if !ok {
		return 1
	}
	currentCfg, exists := currentDoc.flatProfile(name)
	if !exists ||
		validateServerRemovalCheckpointDocument(
			currentDoc,
			name,
			currentCfg,
		) != nil ||
		currentCfg.ServerRemoval.ProfileID !=
			cfg.ServerRemoval.ProfileID {
		printHumanErr(
			"server configuration changed during credential cleanup; preserved the current configuration",
		)
		return 1
	}
	if hasCloud, err :=
		serverProfileContainsCloudState(
			currentDoc,
			name,
		); err != nil || hasCloud {
		printHumanErr(
			"server profile %q gained Home Assistant Cloud state during credential cleanup; preserved it for explicit Cloud cleanup",
			name,
		)
		return 1
	}
	servers, err := documentServersCopy(currentDoc)
	if err != nil {
		printHumanErr(
			"cannot update the server configuration: %v; the checkpoint remains",
			err,
		)
		return 1
	}
	delete(servers, name)
	newDefault := currentDoc.defaultServerName()
	if newDefault == name {
		newDefault = defaultServerProfileName
	}
	if err := writeServersDocument(
		paths,
		currentDoc,
		servers,
		newDefault,
	); err != nil {
		printHumanErr(
			"cannot finalize the server configuration: %v; the checkpoint remains for a safe retry",
			err,
		)
		return 1
	}
	report.printDetails()
	printHumanInfo("Removed server profile %q.", name)
	if newDefault != currentDoc.defaultServerName() {
		printHumanInfo("default_server was reset to %q.", newDefault)
	}
	return 0
}
