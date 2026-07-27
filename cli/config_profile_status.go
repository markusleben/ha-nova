package main

// selectedServerProfileStatus reports the active profile name and how many
// profiles the config defines (best-effort; 1 when unreadable). Doctor uses it
// to name the checked profile on multi-server installs.
func selectedServerProfileStatus(paths runtimePaths) (string, int) {
	count := 1
	if doc, err := loadConfigDocument(paths.ConfigFile); err == nil {
		count = len(doc.profileNames())
	}
	return activeServerProfile(), count
}
