package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// credentialReaders are the functions that hand out a stored secret: the
// device key, a Home Assistant Link token, or the relay token (0.32 spec §5.7).
var credentialReaders = map[string]bool{
	"readDeviceCredential": true, "readDeviceCredentialWithPolicy": true,
	"readPendingDeviceCredential": true, "readPendingDeviceCredentialRecord": true,
	"readPendingDeviceCredentialRecordWithPolicy": true, "readPendingCredentialRecordFromService": true,
	"readCredentialSlot": true, "readCredentialSlotWithPolicy": true,
	"readKeyringDeviceSecret": true, "readKeyringDeviceSecretWithPolicy": true,
	"readKeyringSlotDirect": true, "readPendingKeyringSlotDirect": true,
	"readKeyringDeviceCredentialCleanup": true, "deviceSecretFileGet": true,
	"secretGet": true, "secretGetWithPolicy": true,
	"readRelayAuthToken": true, "readRelayAuthTokenWithPolicy": true,
	"readRelayAuthTokenOverride": true, "readRelayAuthTokenFile": true,
	"readRelayAuthTokenFileOverride": true, "readSecretWithService": true,
	"readLegacyWindowsRelayAuthToken": true,
	"LoadCurrent":                     true, "LoadPending": true, "LoadRetiring": true,
	"readLegacyLinkSecret": true, "decodeOAuthSecretEnvelope": true,
}

// credentialReaderCallers is every production function (or package variable)
// that uses a reader, as file:name.
//   - "reader": store plumbing or a test seam that only hands the secret to
//     its own callers, which are listed here too.
//   - "local": the secret stays on this computer (a presence or equality
//     check, a preflight, a migration step, a delete).
//   - "sends": the secret may leave the process, directly or through what the
//     function returns; it must keep its transport rule: the device key only
//     over its sealed https pin, a Link token only through the verified Link
//     ingress, the relay token over its configured transport.
//
// A new caller fails this test until it is classified here.
var credentialReaderCallers = map[string]string{
	"cloud_authorization_cleanup_plan.go:inspectCloudAuthorizationCleanup":              "sends",
	"cloud_device_preflight.go:inspectCloudDeviceAccess":                                "local",
	"cloud_device_preflight.go:readCloudDeviceForSetup":                                 "reader",
	"cloud_device_preflight.go:readCloudPendingDeviceForSetup":                          "reader",
	"cloud_device_revoke.go:cloudDeviceRevocationAuthorization":                         "sends",
	"cloud_device_revoke_checkpoint_retry.go:validateCheckpointedCloudDeviceCredential": "local",
	"cloud_device_revoke_flow.go:planCloudDeviceRevocation":                             "sends",
	"cloud_device_select.go:selectPendingRemoteCloudDeviceCredential":                   "sends",
	"cloud_manual_device_recovery.go:manuallyConfirmedMissingCloudDeviceCheckpoint":     "local",
	"cloud_manual_revocation_recovery.go:validateManualCloudDeviceSlots":                "local",
	"cloud_oauth_secret_exact_delete.go:DeleteCurrentExact":                             "reader",
	"cloud_oauth_secret_exact_delete.go:DeletePendingExact":                             "reader",
	"cloud_oauth_secret_exact_delete.go:DeletePendingGrantExact":                        "reader",
	"cloud_oauth_secret_exact_delete.go:DeleteRetiringExact":                            "reader",
	"cloud_oauth_secret_store.go:CreatePending":                                         "reader",
	"cloud_oauth_secret_store.go:DeletePending":                                         "reader",
	"cloud_oauth_secret_store.go:PromotePending":                                        "reader",
	"cloud_oauth_secret_store.go:RevokeRetiring":                                        "sends",
	"cloud_oauth_secret_store.go:UpdateCurrent":                                         "reader",
	"cloud_oauth_secret_store.go:UpdatePending":                                         "reader",
	"cloud_reconnect_rollback.go:rollbackCloudReconnectAfterUserConflict":               "sends",
	"cloud_runtime.go:resolveCloudRelayTransport":                                       "sends",
	"cloud_selected_secret_preflight.go:preflightCurrentCloudSecret":                    "local",
	"cloud_selected_secret_preflight.go:preflightCurrentForPendingLifecycle":            "local",
	"cloud_selected_secret_preflight.go:preflightPendingCloudSecret":                    "local",
	"cloud_selected_secret_preflight.go:preflightPromotedCurrentAndRetiringSecret":      "local",
	"cloud_selected_secret_preflight.go:preflightPromotedCurrentSecret":                 "local",
	"cloud_selected_secret_preflight.go:preflightPromotionDependencies":                 "local",
	"cloud_selected_secret_preflight.go:preflightRetiringOrCurrentCloudSecret":          "local",
	"cloud_selected_secret_preflight.go:preflightSelectedCloudSecret":                   "local",
	"cloud_setup_authorization_resume.go:promoteCloudAuthorization":                     "sends",
	"cloud_setup_authorization_resume.go:resumableCloudEnvelope":                        "sends",
	"cloud_setup_production.go:AddAwayWithExistingDevice":                               "sends",
	"cloud_setup_production.go:RetirePrevious":                                          "sends",
	"cloud_setup_production.go:readCloudDeviceCredentialForSetup":                       "reader",
	"cloud_setup_remote.go:establishRemoteCloudDevice":                                  "sends",
	"command_cloud_remove.go:readPendingDeviceCredentialForCloudRemove":                 "reader",
	"command_cloud_remove.go:runCloudRemove":                                            "local",
	"command_cloud_remove_document.go:deletePendingCloudDeviceCredentialWithContext":    "local",
	"command_doctor.go:readRelayAuthTokenForDoctor":                                     "reader",
	"command_server.go:runServerList":                                                   "local",
	"command_server_remove.go:runServerRemove":                                          "sends",
	"command_server_remove.go:secretGetForServerRemove":                                 "reader",
	"command_setup.go:runSetup":                                                         "sends",
	"command_uninstall.go:relayAuthTokenExistsForUninstall":                             "local",
	"device_credential_migration.go:migrateKeyringDeviceCredentialToFile":               "sends",
	"device_credential_migration.go:readKeyringSlotDirect":                              "reader",
	"device_credential_migration.go:readPendingKeyringSlotDirect":                       "reader",
	"device_credential_migration.go:resumeKeyringDeviceCredentialCleanup":               "local",
	"device_credential_pending_record.go:readPendingDeviceCredentialRecord":             "reader",
	"device_credential_pending_record.go:readPendingDeviceCredentialRecordWithPolicy":   "reader",
	"device_credential_residue.go:removeDeviceFileStorageMarkerAndDir":                  "local",
	"device_credential_residue.go:requireEmptyServerCredentialNamespace":                "local",
	"device_credential_retire_flow.go:revokeDeviceCredentialsForRetirement":             "sends",
	"device_credential_retirement.go:validateDeviceCredentialRetirementBindings":        "local",
	"device_credential_retirement.go:writeDeviceCredentialRetirementCheckpoint":         "local",
	"device_credential_storage.go:readKeyringDeviceSecret":                              "reader",
	"device_credential_storage_probe.go:fileStorageCanary":                              "local",
	"device_credential_store.go:promotePendingDeviceCredentialWithPolicy":               "local",
	"device_credential_store.go:readCredentialSlot":                                     "reader",
	"device_credential_store.go:readCredentialSlotWithPolicy":                           "reader",
	"device_credential_store.go:readDeviceCredential":                                   "reader",
	"device_credential_store.go:readDeviceCredentialWithPolicy":                         "reader",
	"device_credential_store.go:readPendingDeviceCredential":                            "reader",
	"doctor_route.go:selectDoctorRoute":                                                 "sends",
	"keyring_darwin.go:readRelayAuthToken":                                              "reader",
	"keyring_darwin.go:readRelayAuthTokenWithPolicy":                                    "reader",
	"keyring_darwin_relay_token.go:snapshotDarwinRelayAuthTokenForSetupPersistence":     "local",
	"keyring_generic.go:secretGet":                                                      "reader",
	"keyring_generic.go:secretGetWithPolicy":                                            "reader",
	"keyring_linux.go:readRelayAuthToken":                                               "reader",
	"keyring_service.go:readRelayAuthTokenFileOverride":                                 "reader",
	"keyring_windows.go:readRelayAuthToken":                                             "reader",
	"legacy_access.go:legacyTokenReadable":                                              "local",
	"pairing_flow.go:resumePendingActivationWithPolicy":                                 "sends",
	"pairing_flow.go:validateLocalDeviceReplacementAllowedWithPolicy":                   "local",
	"pairing_previous_device.go:readPreviousLocalDevice":                                "sends",
	"relay_transport.go:relayFunctionalTransport":                                       "sends",
	"setup_flow_connect.go:stepSecureStorageRecovery":                                   "sends",
	"setup_flow_credentials.go:checkTokenStorage":                                       "sends",
	"setup_flow_credentials.go:prepareCredentials":                                      "sends",
	"setup_interactive.go:readRelayAuthTokenForSetup":                                   "reader",
	"setup_link_repair.go:linkCredentialsPresent":                                       "local",
	"setup_service_credentials.go:disableServiceRelayTokenFile":                         "local",
	"setup_state.go:detectSetupState":                                                   "local",
	"uninstall_device.go:deviceCredentialExistsForUninstall":                            "local",
	"uninstall_device.go:purgeProfileDeviceCredentialWithReport":                        "sends",
	"uninstall_device_checkpoint_validation.go:readPendingCredentialRecordFromService":  "reader",
	"uninstall_device_checkpoint_validation.go:validateCheckpointedProfilePurgeSlot":    "local",
	"uninstall_device_plan.go:inspectProfilePurgeSlotState":                             "local",
	"uninstall_device_service_mode.go:requireNoStrandedKeyringDeviceCredentials":        "local",
	"uninstall_feedback.go:applyUninstallKeyringTokenBestEffort":                        "local",
	"uninstall_feedback.go:applyUninstallTokenPolicy":                                   "local",
	"uninstall_feedback.go:readRelayAuthTokenForUninstall":                              "reader",
	"uninstall_guided_preflight.go:validateUninstallSecureStorageBeforeGuidedTeardown":  "local",
	"uninstall_preflight.go:collectUninstallPreflight":                                  "local",
	"uninstall_purge_inventory.go:requirePurgedCloudCredentialsAbsent":                  "local",
	"uninstall_purge_session.go:purgeRefusedForUnreadableRelayToken":                    "local",
	"uninstall_purge_session.go:purgeRelayTokenRead":                                    "reader",
	"uninstall_purge_session.go:purgeSecureStorageReadError":                            "local",
	"version.go:relayNoticeTransport":                                                   "sends",
	"cloud_oauth_secret_store.go:load":                                                  "reader",
	"legacy_link_cleanup.go:legacyLinkSecretRead":                                       "reader",
	"legacy_link_cleanup.go:recoverLegacyLinkGrant":                                     "sends", // to its own Nabu Casa origin, only to revoke it
	"legacy_link_copies.go:legacyLinkCopiesOf":                                          "reader",
}

func TestEveryCredentialReaderCallerIsClassified(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	readers := map[string]bool{}
	for name := range credentialReaders {
		readers[name] = true
	}
	uses := func(n ast.Node) bool {
		hit := false
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				hit = hit || readers[x.Name]
			case *ast.SelectorExpr:
				hit = hit || readers[x.Sel.Name]
			}
			return !hit
		})
		return hit
	}
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if parsed[file], err = parser.ParseFile(fset, file, src, 0); err != nil {
			t.Fatal(err)
		}
	}
	// A package variable holding a reader (a test seam) is a reader too, so
	// its callers are listed as well.
	found := map[string]bool{}
	for grew := true; grew; {
		grew = false
		for file, f := range parsed {
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok || readers[value.Names[0].Name] {
						continue
					}
					for _, v := range value.Values {
						if uses(v) {
							readers[value.Names[0].Name] = true
							found[file+":"+value.Names[0].Name] = true
							grew = true
						}
					}
				}
			}
		}
	}
	for file, f := range parsed {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && uses(fn.Body) {
				found[file+":"+fn.Name.Name] = true
			}
		}
	}
	var unlisted, gone []string
	for caller := range found {
		if _, ok := credentialReaderCallers[caller]; !ok {
			unlisted = append(unlisted, caller)
		}
	}
	for caller, kind := range credentialReaderCallers {
		if !found[caller] {
			gone = append(gone, caller)
		}
		if kind != "reader" && kind != "local" && kind != "sends" {
			t.Errorf("%s: kind %q, want reader, local or sends", caller, kind)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(gone)
	if len(unlisted) > 0 {
		t.Errorf("new credential readers; classify each in credentialReaderCallers as reader, local or sends, and keep its transport rule:\n%s", strings.Join(unlisted, "\n"))
	}
	if len(gone) > 0 {
		t.Errorf("listed callers no longer read a credential; remove them:\n%s", strings.Join(gone, "\n"))
	}
}
