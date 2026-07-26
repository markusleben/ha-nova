package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fullPurgeCredentialConfig = `{
	"schema_version":3,
	"default_server":"default",
	"servers":{
		"default":{
			"profile_id":"profile-default",
			"relay_secure_base_url":"https://default.local:8792",
			"relay_spki_pin":"pin-default",
			"route_policy":"local"
		}
	}
}`

const fullPurgeEmptyCredentialConfig = `{
	"schema_version":3,
	"default_server":"default",
	"servers":{
		"default":{
			"profile_id":"profile-default",
			"route_policy":"local"
		}
	}
}`

func TestFullPurgeRejectsRecreatedUninventoriedRawCredential(
	t *testing.T,
) {
	for _, service := range []string{
		deviceCredentialServiceForProfile("orphan"),
		deviceCredentialPendingServiceForProfile("orphan"),
	} {
		t.Run(service, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				fullPurgeEmptyCredentialConfig,
			)
			targets, err := collectProfilePurgeTargets(paths)
			if err != nil {
				t.Fatal(err)
			}
			rawPath, err := deviceSecretFilePath(service)
			if err != nil {
				t.Fatal(err)
			}
			replacement := validCredential(221)
			previousHook := profilePurgeFinalProofHook
			profilePurgeFinalProofHook = func() error {
				if err := os.MkdirAll(
					filepath.Dir(rawPath),
					0o700,
				); err != nil {
					return err
				}
				return os.WriteFile(
					rawPath,
					[]byte(replacement),
					0o600,
				)
			}
			t.Cleanup(func() {
				profilePurgeFinalProofHook = previousHook
			})
			err = purgeAllDeviceCredentialsWithReport(
				targets,
				&uninstallReport{},
				nil,
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"reappeared after full cleanup",
				) {
				t.Fatalf("full purge error = %v", err)
			}
			raw, readErr := os.ReadFile(rawPath)
			if readErr != nil || string(raw) != replacement {
				t.Fatalf(
					"replacement=%q err=%v",
					raw,
					readErr,
				)
			}
		})
	}
}

func TestFullUninstallPreservesConfigWhenCredentialReappears(
	t *testing.T,
) {
	paths := setupServerCommandTest(
		t,
		fullPurgeCredentialConfig,
	)
	original := validCredential(222)
	if err := secretSet(
		deviceCredentialService,
		original,
	); err != nil {
		t.Fatal(err)
	}
	stubServerRevoke(t)
	replacement := validCredential(223)
	err := finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		func(step string) error {
			if step != "config_cleanup" {
				return nil
			}
			return secretSet(
				deviceCredentialService,
				replacement,
			)
		},
		false,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"reappeared before config cleanup",
		) {
		t.Fatalf("full uninstall error = %v", err)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config inventory was removed: %v", statErr)
	}
	actual, readErr := secretGet(deviceCredentialService)
	if readErr != nil || actual != replacement {
		t.Fatalf(
			"replacement=%q err=%v",
			actual,
			readErr,
		)
	}
}

func TestFullUninstallPurgeRemovesUnreadableCredentialSlots(
	t *testing.T,
) {
	for _, slot := range []string{"current", "pending"} {
		t.Run(slot, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				fullPurgeCredentialConfig,
			)
			if slot == "pending" {
				if err := secretSet(
					deviceCredentialService,
					validCredential(224),
				); err != nil {
					t.Fatal(err)
				}
			}
			service := deviceCredentialService
			if slot == "pending" {
				service = deviceCredentialPendingService
			}
			if err := secretSet(service, "malformed"); err != nil {
				t.Fatal(err)
			}
			stubServerRevoke(t)
			report := &uninstallReport{}
			if err := finalizeLocalUninstallWithProgress(
				paths,
				installState{},
				report,
				uninstallModePurge,
				nil,
				false,
			); err != nil {
				t.Fatalf("full uninstall: %v", err)
			}
			if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
				t.Fatalf("config remains after purge: %v", err)
			}
			if _, err := secretGet(service); err != errSecretNotFound {
				t.Fatalf(
					"malformed %s slot remains: %v",
					slot,
					err,
				)
			}
			if notes := strings.Join(report.notes, "\n"); !strings.Contains(
				notes,
				"unreadable and was removed without revoking",
			) {
				t.Fatalf("missing honest report: %q", notes)
			}
		})
	}
}
