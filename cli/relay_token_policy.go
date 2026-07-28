package main

func writeRelayAuthTokenInteractive(token string) error {
	return writeRelayAuthTokenWithPolicy(token, SecretStoreAllowUI)
}

func deleteRelayAuthTokenInteractive() error {
	return deleteRelayAuthTokenWithPolicy(SecretStoreAllowUI)
}

func restoreRelayAuthTokenInteractive(
	previousToken string,
	hadPreviousToken, tokenChanged bool,
) error {
	if !tokenChanged {
		return nil
	}
	if hadPreviousToken {
		return writeRelayAuthTokenInteractive(previousToken)
	}
	return deleteRelayAuthTokenInteractive()
}
