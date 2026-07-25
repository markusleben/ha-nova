package main

// Host-safe tests exercise the complete Cloud implementation with injected
// stores and servers. Production builds remain fail-closed unless release
// metadata enables a validated platform; this test-only init is never compiled
// into a shipped binary.
func init() {
	cloudRemoteBuildIdentityForRuntime = func() cloudRemoteBuildIdentity {
		return cloudRemoteBuildIdentity{
			Development: true,
			AppSlug:     "local_ha_nova_test",
		}
	}
}
