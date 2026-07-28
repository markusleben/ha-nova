//go:build !darwin

package main

func writeRelayAuthTokenWithPolicy(
	token string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	return writeRelayAuthToken(token)
}

func deleteRelayAuthTokenWithPolicy(ui SecretStoreUIPolicy) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	return deleteRelayAuthToken()
}
