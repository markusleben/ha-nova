package main

import (
	"bufio"
	"fmt"
	"io"
)

func enableServiceRelayTokenFile(paths runtimePaths, cfg runtimeConfig) runtimeConfig {
	if cfg.RelayTokenFile == "" {
		cfg.RelayTokenFile = defaultRelayAuthTokenFile(paths)
	}
	return cfg
}

func withRelayAuthTokenFileOverride(path string) func() {
	previous := relayAuthTokenFilePathOverride
	relayAuthTokenFilePathOverride = path
	return func() {
		relayAuthTokenFilePathOverride = previous
	}
}

func selectedClientsServiceCredentialHint(paths runtimePaths, selectedClients []string) (clientRegistryServiceCredentials, string, bool, error) {
	client, ok, err := selectedClientsSupportServiceCredentials(paths, selectedClients)
	if err != nil || !ok {
		return clientRegistryServiceCredentials{}, "", ok, err
	}
	serviceCredentials := clientSetupForCurrentOS(client).ServiceCredentials
	if serviceCredentials == nil {
		return clientRegistryServiceCredentials{}, "", false, nil
	}
	return *serviceCredentials, client.Label, true, nil
}

func requireSelectedClientServiceCredentials(paths runtimePaths, selectedClients []string) error {
	_, _, ok, err := selectedClientsServiceCredentialHint(paths, selectedClients)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if len(selectedClients) == 1 {
		return fmt.Errorf("%s does not support service credentials", setupClientLabel(selectedClients[0]))
	}
	return fmt.Errorf("none of the selected clients support service credentials")
}

func shouldOfferServiceCredentials(tokenStorageErr error) bool {
	if tokenStorageErr != nil {
		return isDesktopKeyringSessionUnavailableError(tokenStorageErr) ||
			isDesktopKeyringLockedError(tokenStorageErr) ||
			isDesktopKeyringInitializationRequiredError(tokenStorageErr) ||
			isDesktopKeyringSetupRequiredError(tokenStorageErr)
	}
	return false
}

func promptSetupServiceCredentialsInteractive(reader *bufio.Reader, out io.Writer, serviceCredentials clientRegistryServiceCredentials, clientLabel string) (bool, error) {
	label := serviceCredentials.Label
	if label == "" {
		label = "Service / gateway mode"
	}
	renderSetupParagraph(out,
		fmt.Sprintf("%s is available for %s.", label, clientLabel),
		serviceCredentials.Help,
	)
	renderSetupParagraphTight(out, "Choose yes for SSH, systemd, headless, or gateway sessions. Choose no for a normal desktop terminal with an unlocked keyring.")
	return promptYesNoFromReader(reader, out, "Use service/gateway token file instead of desktop keyring?", false)
}

func doctorServiceCredentialRecoveryHint(paths runtimePaths, state installState, err error) string {
	if !shouldOfferServiceCredentials(err) {
		return ""
	}
	clients, loadErr := loadClientRegistry(paths)
	if loadErr != nil {
		return ""
	}
	for _, client := range clients {
		if !clientSupportsServiceCredentials(client) {
			continue
		}
		status := evaluateClientStatus(paths, state, client)
		if status.Configured || status.Attached {
			return fmt.Sprintf("Recovery: run `ha-nova setup --service %s` if %s runs without an unlocked desktop keyring.", client.ID, client.Label)
		}
	}
	return ""
}
