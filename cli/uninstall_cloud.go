package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type cloudPurgeTarget struct {
	profileName string
	profileID   string
	config      runtimeConfig
	recovery    cloudRecoveryCheckpointExpectation
}

func purgeCloudAuthorizationsForUninstall(
	paths runtimePaths,
	report *uninstallReport,
	relayAlreadyRemoved bool,
) error {
	targets, err := collectCloudPurgeTargets(paths.ConfigFile)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, target := range targets {
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err != nil {
			return cloudPurgeFailure(paths, target, fmt.Errorf(
				"open Cloud credentials for server %q: %w",
				target.profileName,
				err,
			))
		}
		hadAuthorization, err := cloudAuthorizationExists(
			ctx,
			store,
		)
		if err != nil {
			return cloudPurgeFailure(paths, target, fmt.Errorf(
				"inspect Cloud credentials for server %q: %w",
				target.profileName,
				err,
			))
		}
		if _, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
			ctx,
			target.config,
			target.profileName,
			store,
			report,
			relayAlreadyRemoved,
		); err != nil {
			return cloudPurgeFailure(paths, target, fmt.Errorf(
				"revoke Cloud device for server %q: %w",
				target.profileName,
				err,
			))
		}
		if err := revokeAllCloudAuthorizations(ctx, store); err != nil {
			return cloudPurgeFailure(paths, target, fmt.Errorf(
				"revoke Cloud authorization for server %q: %w",
				target.profileName,
				err,
			))
		}
		if err := deleteRevokedCloudAuthorizations(ctx, store); err != nil {
			return cloudPurgeFailure(paths, target, fmt.Errorf(
				"remove Cloud credentials for server %q: %w",
				target.profileName,
				err,
			))
		}
		if hadAuthorization && report != nil {
			report.addRemoved(
				fmt.Sprintf(
					"Home Assistant Cloud authorization (server %q)",
					target.profileName,
				),
			)
		}
	}
	return nil
}

// Uninstall calls this while holding the client mutation lock. Persisting the
// per-profile hold here ensures a multi-profile purge cannot return with an
// ambiguous authorization outcome represented only in process memory.
func cloudPurgeFailure(
	paths runtimePaths,
	target cloudPurgeTarget,
	cause error,
) error {
	_, err := checkpointCloudRecoveryHoldUnlocked(
		paths,
		target.recovery,
		cause,
	)
	if err != nil {
		return errors.Join(
			cause,
			fmt.Errorf("persist Cloud recovery safety hold: %w", err),
		)
	}
	return cause
}

func cloudAuthorizationExists(
	ctx context.Context,
	store OAuthSecretStore,
) (bool, error) {
	if _, exists, err := store.LoadRetiring(
		ctx,
		SecretStoreForbidUI,
	); err != nil || exists {
		return exists, err
	}
	if _, exists, err := store.LoadPending(
		ctx,
		SecretStoreForbidUI,
	); err != nil || exists {
		return exists, err
	}
	_, exists, err := store.LoadCurrent(ctx, SecretStoreForbidUI)
	return exists, err
}

func collectCloudPurgeTargets(path string) ([]cloudPurgeTarget, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Cloud purge configuration: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var top map[string]json.RawMessage
	if err := decoder.Decode(&top); err != nil || ensureJSONEOF(decoder) != nil {
		return nil, fmt.Errorf(
			"cannot safely inspect Cloud credentials in config.json",
		)
	}
	if top == nil {
		return nil, fmt.Errorf(
			"cannot safely inspect Cloud credentials in config.json",
		)
	}
	if rawSchema, exists := top["schema_version"]; exists {
		var schema int
		if err := json.Unmarshal(rawSchema, &schema); err != nil {
			return nil, fmt.Errorf(
				"cannot safely inspect config schema_version",
			)
		}
		if schema > configSchemaVersion {
			return nil, fmt.Errorf(
				"config schema_version %d is newer than this HA NOVA build supports (%d); update HA NOVA before purging it",
				schema,
				configSchemaVersion,
			)
		}
	}

	rawProfiles := map[string]json.RawMessage{}
	if rawServers, ok := top["servers"]; ok {
		if bytes.Equal(bytes.TrimSpace(rawServers), []byte("null")) {
			return nil, fmt.Errorf(
				"cannot safely inspect Cloud credentials in the servers map",
			)
		}
		if err := json.Unmarshal(rawServers, &rawProfiles); err != nil {
			return nil, fmt.Errorf(
				"cannot safely inspect Cloud credentials in the servers map",
			)
		}
		if rawProfiles == nil {
			return nil, fmt.Errorf(
				"cannot safely inspect Cloud credentials in the servers map",
			)
		}
		if rawCloud, exists := top["cloud"]; exists &&
			!bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
			return nil, fmt.Errorf(
				"cannot safely identify top-level Cloud credentials alongside the servers map",
			)
		}
	} else {
		raw, err := json.Marshal(top)
		if err != nil {
			return nil, err
		}
		rawProfiles[defaultServerProfileName] = raw
	}

	names := make([]string, 0, len(rawProfiles))
	for name := range rawProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := validateExistingServerProfileIDs(rawProfiles); err != nil {
		return nil, fmt.Errorf(
			"cannot safely purge Cloud credentials: %w",
			err,
		)
	}
	targets := make([]cloudPurgeTarget, 0, len(names))
	for _, name := range names {
		target, configured, err := cloudPurgeTargetFromRaw(
			name,
			rawProfiles[name],
		)
		if err != nil {
			return nil, err
		}
		if !configured {
			continue
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func cloudPurgeTargetFromRaw(
	name string,
	raw json.RawMessage,
) (cloudPurgeTarget, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return cloudPurgeTarget{}, false, fmt.Errorf(
			"cannot safely inspect Cloud credentials for server %q",
			name,
		)
	}
	if fields == nil {
		return cloudPurgeTarget{}, false, fmt.Errorf(
			"cannot safely inspect Cloud credentials for server %q",
			name,
		)
	}
	rawCloud, configured := fields["cloud"]
	if !configured ||
		len(bytes.TrimSpace(rawCloud)) == 0 ||
		bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
		return cloudPurgeTarget{}, false, nil
	}
	if err := validateKnownCloudRemovalShape(name, rawCloud); err != nil {
		return cloudPurgeTarget{}, false, err
	}
	var profileID string
	if rawProfileID, ok := fields["profile_id"]; ok {
		_ = json.Unmarshal(rawProfileID, &profileID)
	}
	profileID = strings.TrimSpace(profileID)
	if validateProfileID(profileID) != nil {
		return cloudPurgeTarget{}, false, fmt.Errorf(
			"cannot safely identify Cloud credentials for server %q",
			name,
		)
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cloudPurgeTarget{}, false, fmt.Errorf(
			"cannot safely inspect Cloud configuration for server %q",
			name,
		)
	}
	cfg.ProfileID = profileID
	return cloudPurgeTarget{
		profileName: name,
		profileID:   profileID,
		config:      cfg,
		recovery: newCloudRecoveryCheckpointExpectation(
			name,
			profileID,
			raw,
		),
	}, true, nil
}

func validateKnownCloudRemovalShape(
	name string,
	raw json.RawMessage,
) error {
	var lifecycle map[string]json.RawMessage
	if err := json.Unmarshal(raw, &lifecycle); err != nil || lifecycle == nil {
		return unknownCloudRemovalShape(name)
	}
	for field := range lifecycle {
		if !knownCloudRemovalLifecycleField(field) {
			return unknownCloudRemovalShape(name)
		}
	}
	if rawState, exists := lifecycle["state"]; exists {
		var state cloudLifecycleState
		if err := json.Unmarshal(rawState, &state); err != nil ||
			!knownCloudLifecycleState(state) {
			return unknownCloudRemovalShape(name)
		}
	}
	if rawHold, exists := lifecycle["recovery_hold"]; exists &&
		!bytes.Equal(bytes.TrimSpace(rawHold), []byte("null")) {
		var hold cloudRecoveryHold
		if err := json.Unmarshal(rawHold, &hold); err != nil ||
			validateCloudRecoveryHold(&hold) != nil {
			return unknownCloudRemovalShape(name)
		}
	}
	for _, slot := range []string{"current", "pending"} {
		rawSlot, exists := lifecycle[slot]
		if !exists ||
			bytes.Equal(bytes.TrimSpace(rawSlot), []byte("null")) {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawSlot, &fields); err != nil || fields == nil {
			return unknownCloudRemovalShape(name)
		}
		for field := range fields {
			if !knownCloudRemovalConnectionField(field) {
				return unknownCloudRemovalShape(name)
			}
		}
	}
	return nil
}

func knownCloudRemovalLifecycleField(field string) bool {
	switch field {
	case "state", "current", "pending", "device_activation_started", "recovery_hold":
		return true
	default:
		return false
	}
}

func knownCloudRemovalConnectionField(field string) bool {
	switch field {
	case "origin",
		"canonical_origin",
		"oauth_client_id",
		"credential_generation",
		"ha_user_id":
		return true
	default:
		return false
	}
}

func knownCloudLifecycleState(state cloudLifecycleState) bool {
	switch state {
	case "",
		cloudStateAuthorizing,
		cloudStateTokenStored,
		cloudStateCloudVerified,
		cloudStateDeviceBoundOrPaired,
		cloudStateRollingBack,
		cloudStateCommitted,
		cloudStateRetiringPrevious,
		cloudStateReady:
		return true
	default:
		return false
	}
}

func unknownCloudRemovalShape(name string) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: fmt.Sprintf(
			"server %q contains Cloud state this version cannot safely remove; update HA NOVA before retrying",
			name,
		),
	}
}

func cloudConfigurationExistsForUninstall(path string) bool {
	targets, err := collectCloudPurgeTargets(path)
	// Standard uninstall preserves config and credentials. If the document is
	// too new or malformed to inspect safely, warn conservatively that Cloud
	// authorization is being kept instead of implying it does not exist.
	return err != nil || len(targets) > 0
}
