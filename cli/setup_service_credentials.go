package main

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"
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

// disableServiceRelayTokenFile returns token storage to the OS keyring when
// setup runs without --service: the documented desktop path must not keep
// using a previously configured service token file. It clears the config
// field, suppresses token-file routing for the rest of the run, and returns
// the absolute former file path plus its token (best effort) so setup can
// migrate the credential and remove the file after success.
func disableServiceRelayTokenFile(paths runtimePaths, cfg runtimeConfig) (runtimeConfig, string, string, func()) {
	path := strings.TrimSpace(cfg.RelayTokenFile)
	if path == "" {
		return cfg, "", "", func() {}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.ConfigDir, path)
	}
	path = filepath.Clean(path)
	formerToken := ""
	if token, err := readRelayAuthTokenFile(path); err == nil {
		formerToken = token
	}
	cfg.RelayTokenFile = ""
	return cfg, path, formerToken, withRelayAuthTokenFileSuppressed()
}

// finalizeServiceTokenFileMigration force-writes the active token into the
// OS keyring before removing the former service token file, so the
// migration can never delete the only stored copy of the credential —
// regardless of whether the persistence layer considered the token
// unchanged (it compares values, not destination stores).
func finalizeServiceTokenFileMigration(path, token string) {
	if path == "" {
		return
	}
	if strings.TrimSpace(token) != "" {
		if err := writeRelayAuthToken(token); err != nil {
			printHumanWarn("Keeping the former service token file %s: could not store the token in OS secure storage: %s", path, err)
			return
		}
	}
	cleanupFormerServiceTokenFile(path)
}

// cleanupFormerServiceTokenFile removes the now-orphaned service token file
// after a successful desktop-mode setup so no secret copy stays behind.
func cleanupFormerServiceTokenFile(path string) {
	if path == "" {
		return
	}
	if err := deleteRelayAuthTokenFile(path); err != nil {
		printHumanWarn("Could not remove the former service token file %s: %s", path, err)
		return
	}
	printHumanInfo("Token storage returned to OS secure storage; removed the former service token file.")
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
			isDesktopKeyringUnavailableError(tokenStorageErr) ||
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
