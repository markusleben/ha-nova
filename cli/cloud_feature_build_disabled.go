//go:build cloudremote_disabled && !cloudremote_official && !cloudremote_dev

package main

func compiledCloudRemoteBuildIdentity() cloudRemoteBuildIdentity {
	return cloudRemoteBuildIdentity{Disabled: true}
}
