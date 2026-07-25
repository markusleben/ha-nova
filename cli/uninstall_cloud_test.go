package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCloudPurgeConfig(t *testing.T, path, profileID string) {
	t.Helper()
	data := `{
		"schema_version": 3,
		"servers": {
			"default": {
				"profile_id": "` + profileID + `",
				"route_policy": "cloud",
				"relay_secure_base_url": "https://unit.local:8792",
				"relay_spki_pin": "pin",
				"cloud": {
					"state": "ready",
					"current": {
						"origin": "https://unit.ui.nabu.casa",
						"canonical_origin": "https://unit.ui.nabu.casa",
						"oauth_client_id": "http://127.0.0.1:43123/ha-nova",
						"credential_generation": "0123456789abcdef0123456789abcdef",
						"ha_user_id": "user-1"
					}
				}
			}
		}
	}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCloudPurgeRevokesBeforeDeletingNativeSecrets(t *testing.T) {
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	writeCloudPurgeConfig(t, paths.ConfigFile, "default")
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "uninstall-refresh")
	backend.policies = nil

	oldStore := newCloudSecretStoreForCLI
	oldRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != "default" {
			t.Fatalf("profile id = %q", profileID)
		}
		return store, nil
	}
	var revoked []string
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		revoked = append(revoked, envelope.Generation)
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldRevoke
	})

	report := &uninstallReport{}
	if err := purgeCloudAuthorizationsForUninstall(paths, report, nil); err != nil {
		t.Fatalf("purge Cloud authorization: %v", err)
	}
	if len(revoked) != 1 || revoked[0] != current.Generation {
		t.Fatalf("revoked generations = %v", revoked)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("current secret survived purge: exists=%v err=%v", exists, err)
	}
	for _, policy := range backend.policies {
		if policy != SecretStoreForbidUI {
			t.Fatalf("purge used UI policy %q", policy)
		}
	}
	if len(report.removed) != 1 ||
		!strings.Contains(report.removed[0], "Cloud authorization") {
		t.Fatalf("purge report = %+v", report.removed)
	}
}

func TestFullPurgeStopsBeforeLocalRemovalWhenCloudRevocationCannotStart(t *testing.T) {
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	writeCloudPurgeConfig(t, paths.ConfigFile, "profile-locked")
	oldStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		return nil, errors.New("native secure storage locked")
	}
	t.Cleanup(func() { newCloudSecretStoreForCLI = oldStore })

	err := finalizeLocalUninstall(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "Cloud authorization") {
		t.Fatalf("full purge error = %v", err)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config was removed before Cloud revocation: %v", statErr)
	}
}

func TestCloudPurgeTargetExtractionCoversAllProfilesAndRejectsMissingIdentity(t *testing.T) {
	path := t.TempDir() + "/config.json"
	data := `{
		"servers": {
			"cabin": {"profile_id":"profile-cabin","cloud":{"state":"authorizing"}},
			"default": {"profile_id":"profile-default","cloud":{"state":"ready"}},
			"local": {"profile_id":"profile-local","route_policy":"local"}
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := collectCloudPurgeTargets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 ||
		targets[0].profileName != "cabin" ||
		targets[1].profileName != "default" {
		t.Fatalf("targets = %+v", targets)
	}

	if err := os.WriteFile(
		path,
		[]byte(`{"servers":{"default":{"cloud":{"state":"ready"}}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := collectCloudPurgeTargets(path); err == nil {
		t.Fatal("Cloud config without profile identity was accepted")
	}
}

func TestCloudPurgeTargetExtractionRejectsDuplicateProfileIdentity(
	t *testing.T,
) {
	path := t.TempDir() + "/config.json"
	data := `{"servers":{
		"cabin":{"profile_id":"profile-shared","cloud":{"state":"authorizing"}},
		"default":{"profile_id":"profile-shared","cloud":{"state":"ready"}}
	}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := collectCloudPurgeTargets(path); err == nil ||
		!strings.Contains(err.Error(), "share profile_id") {
		t.Fatalf("duplicate profile identity error = %v", err)
	}
}

func TestCloudPurgeTargetExtractionRejectsAmbiguousJSONShapes(t *testing.T) {
	path := t.TempDir() + "/config.json"
	for _, test := range []struct {
		name string
		raw  string
	}{
		{
			name: "null document",
			raw:  `null`,
		},
		{
			name: "future schema",
			raw: `{
				"schema_version":4,
				"servers":{"default":{
					"profile_id":"profile-default",
					"cloud":{"state":"authorizing"}
				}}
			}`,
		},
		{
			name: "null servers map",
			raw:  `{"servers":null}`,
		},
		{
			name: "null profile",
			raw:  `{"servers":{"default":null}}`,
		},
		{
			name: "top-level Cloud shadow beside servers",
			raw: `{
				"profile_id":"profile-shadow",
				"cloud":{"state":"authorizing"},
				"servers":{}
			}`,
		},
		{
			name: "unknown Cloud lifecycle field",
			raw: `{"servers":{"default":{
				"profile_id":"profile-default",
				"cloud":{"state":"ready","future_secret_slot":{}}
			}}}`,
		},
		{
			name: "unknown Cloud lifecycle state",
			raw: `{"servers":{"default":{
				"profile_id":"profile-default",
				"cloud":{"state":"future"}
			}}}`,
		},
		{
			name: "unknown Cloud connection field",
			raw: `{"servers":{"default":{
				"profile_id":"profile-default",
				"cloud":{"state":"ready","current":{"future_binding":"opaque"}}
			}}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := collectCloudPurgeTargets(path); err == nil {
				t.Fatalf("ambiguous config was accepted: %s", test.raw)
			}
		})
	}
}

func TestMultiProfileCloudPurgePersistsHoldForFailingProfile(t *testing.T) {
	paths := writeTestConfigFile(t, `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"cabin":{
				"profile_id":"profile-cabin-purge",
				"route_policy":"cloud",
				"cloud":{"state":"authorizing"}
			},
			"default":{
				"profile_id":"profile-default-purge",
				"route_policy":"cloud",
				"cloud":{"state":"authorizing"}
			}
		}
	}`)
	cabinStore := newTestOAuthStore(t, newMemoryOAuthSecretBackend())
	oldStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID == "profile-cabin-purge" {
			return cabinStore, nil
		}
		return nil, newCloudError(
			CloudErrOAuthOutcomeUnknown,
			"open ambiguous default-profile authorization",
			nil,
		)
	}
	t.Cleanup(func() { newCloudSecretStoreForCLI = oldStore })

	purgeErr := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
		nil,
	)
	if purgeErr == nil {
		t.Fatal("ambiguous multi-profile purge succeeded")
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cabin, ok := doc.flatProfile("cabin")
	if !ok {
		t.Fatal("cabin profile disappeared")
	}
	failed, ok := doc.flatProfile(defaultServerProfileName)
	if !ok {
		t.Fatal("default profile disappeared")
	}
	if cabin.Cloud == nil || cabin.Cloud.RecoveryHold != nil {
		t.Fatalf("successful sibling was held: %+v", cabin.Cloud)
	}
	if failed.Cloud == nil ||
		failed.Cloud.RecoveryHold == nil ||
		failed.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf(
			"failing profile has no durable hold: %+v; purge error: %v",
			failed.Cloud,
			purgeErr,
		)
	}
}
