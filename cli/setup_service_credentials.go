package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
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
// setup runs without --service and the keyring is usable here: the documented
// desktop path must not keep using a previously configured service token
// file. Without a usable keyring the file stays (see below). It clears the config
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
	// The token file is the owner's explicit earlier choice. Return to the
	// keyring only where the keyring works; otherwise keep the file instead of
	// failing client wiring on storage this session cannot reach.
	restore := withRelayAuthTokenFileSuppressed()
	keyringErr := relayAuthTokenSetupPreflightForSetup()
	restore()
	if shouldOfferServiceCredentials(keyringErr) {
		printHumanInfo("Keeping the service token file %s: secure storage is locked or unavailable in this session. To move the token into secure storage, unlock it (or use the desktop session) and rerun `ha-nova setup`.", path)
		return cfg, "", "", func() {}
	}
	formerToken := ""
	if token, err := readRelayAuthTokenFile(path); err == nil {
		formerToken = token
	}
	cfg.RelayTokenFile = ""
	cleanupPath := path
	configDir := filepath.Clean(paths.ConfigDir)
	if rel, err := filepath.Rel(configDir, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		// User-managed token file outside the managed config directory:
		// migrate the credential, but never delete the file — the same
		// boundary purge applies for external paths.
		cleanupPath = ""
		printHumanInfo("Keeping the external relay token file in place: %s", path)
	}
	return cfg, cleanupPath, formerToken, withRelayAuthTokenFileSuppressed()
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

// setupTokenStorageHint adds the service-mode way out to a non-interactive
// token-storage failure when the selected client supports it.
func setupTokenStorageHint(paths runtimePaths, selectedClients []string, err error) string {
	if hint := setupSecureStorageRecoveryHint(err); hint != "" {
		return hint
	}
	if !shouldOfferServiceCredentials(err) || len(selectedClients) != 1 {
		return ""
	}
	if _, _, ok, hintErr := selectedClientsServiceCredentialHint(paths, selectedClients); hintErr != nil || !ok {
		return ""
	}
	return fmt.Sprintf("Headless or SSH session: run `ha-nova setup --service %s` to keep the relay token in a protected file instead.", selectedClients[0])
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

// offerServiceCredentialsQuestion: only where the keyring can't be unlocked
// from here (SSH, no desktop). A desktop terminal gets the unlock offer
// instead, never this question (lane D4).
func offerServiceCredentialsQuestion(tokenStorageErr error) bool {
	return shouldOfferServiceCredentials(tokenStorageErr) && !setupSecureStorageRecoveryAvailableNow(tokenStorageErr)
}

func promptSetupServiceCredentialsInteractive(reader *bufio.Reader, out io.Writer, clientLabel string) (bool, error) {
	renderSetupParagraph(out,
		"HA NOVA can't use this computer's keyring from here (for example over SSH, or when no one is logged in to the desktop).",
		fmt.Sprintf("It can keep its key for %s in a protected file instead. Choose this if %s runs while no one is logged in to this computer's desktop.", clientLabel, clientLabel),
	)
	return promptYesNoFromReader(reader, out, "Keep the key in a protected file instead?", false)
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
