//go:build cloudremote_dev && !cloudremote_official && !cloudremote_disabled

package main

// cloudRemoteDevAppSlug exists only in an explicitly tagged development build.
// The strict local_ slug is linker-stamped by the developer build command.
var cloudRemoteDevAppSlug string

func compiledCloudRemoteBuildIdentity() cloudRemoteBuildIdentity {
	return cloudRemoteBuildIdentity{
		Development: true,
		AppSlug:     cloudRemoteDevAppSlug,
	}
}
