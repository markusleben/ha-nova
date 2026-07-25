//go:build !cloudremote_official && !cloudremote_dev && !cloudremote_disabled

package main

func compiledCloudRemoteBuildIdentity() cloudRemoteBuildIdentity {
	return cloudRemoteBuildIdentity{Disabled: true}
}
