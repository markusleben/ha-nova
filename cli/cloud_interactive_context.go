package main

import "context"

// Human OAuth and Owner-pairing steps have their own lifecycle: OAuth is
// bounded by its loopback timeout, while terminal input may wait as long as the
// user needs. Network and native-storage operations remain individually
// bounded. An outer deadline spanning all phases would expire valid work after
// a slow sign-in and turn the final pairing step into a false failure.
func newInteractiveCloudSetupContext() (
	context.Context,
	context.CancelFunc,
) {
	ctx, cancel := context.WithCancel(context.Background())
	return withCloudSecretAccessHolder(ctx), cancel
}
