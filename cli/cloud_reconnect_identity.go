package main

func rejectCloudReconnectUserChange(
	cfg runtimeConfig,
	verifiedHAUserID string,
) error {
	if cfg.Cloud == nil || cfg.Cloud.Current == nil ||
		cfg.Cloud.Current.HAUserID == "" ||
		cfg.Cloud.Current.HAUserID == verifiedHAUserID {
		return nil
	}
	return newCloudError(
		CloudErrDeviceUserConflict,
		"verify Cloud reconnect user",
		nil,
	)
}
