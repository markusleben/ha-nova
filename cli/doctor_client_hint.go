package main

import "fmt"

// doctorClientNotLoaded: why an AI app found on this computer doesn't load
// HA NOVA.
func doctorClientNotLoaded(client clientStatus) string {
	if client.PluginOutdated {
		return fmt.Sprintf("%s still uses an older version of HA NOVA", client.Label)
	}
	return fmt.Sprintf("%s doesn't load HA NOVA right now", client.Label)
}

func doctorClientRepairHint(client clientStatus, installSource string) string {
	setupCommand := doctorClientSetupCommand(client.ID)
	switch {
	case !client.RuntimeDetected:
		return fmt.Sprintf(
			"To fix: install or open %s, then run `%s`.",
			client.Label,
			setupCommand,
		)
	case installSource == installSourceDev:
		return fmt.Sprintf(
			"To fix: run `npm run dev:sync` or `%s`.",
			setupCommand,
		)
	default:
		return fmt.Sprintf("To fix: run `%s`.", setupCommand)
	}
}

func doctorClientSetupCommand(clientID string) string {
	if activeServerProfile() != defaultServerProfileName {
		return fmt.Sprintf(
			"ha-nova setup --server %s %s",
			activeServerProfile(),
			clientID,
		)
	}
	return fmt.Sprintf("ha-nova setup %s", clientID)
}
