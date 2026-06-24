package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type installStatusReport struct {
	SchemaVersion      int                      `json:"schema_version"`
	EffectiveVersion   string                   `json:"effective_version"`
	InstallSource      string                   `json:"install_source"`
	Binary             installStatusBinary      `json:"binary"`
	Bundle             installStatusBundle      `json:"bundle"`
	VersionFile        installStatusVersionFile `json:"version_file"`
	State              installStatusState       `json:"state"`
	Clients            []installStatusClient    `json:"clients"`
	InactiveArtifacts  []installStatusArtifact  `json:"inactive_artifacts"`
	ActiveDriftClients []string                 `json:"active_drift_clients"`
}

type installStatusBinary struct {
	Version      string `json:"version"`
	Display      string `json:"display"`
	BuildChannel string `json:"build_channel,omitempty"`
	BuildStamp   string `json:"build_stamp,omitempty"`
}

type installStatusBundle struct {
	Path                string `json:"path"`
	Present             bool   `json:"present"`
	BundleFormatVersion int    `json:"bundle_format_version,omitempty"`
	Version             string `json:"version,omitempty"`
	OS                  string `json:"os,omitempty"`
	Arch                string `json:"arch,omitempty"`
	BinaryName          string `json:"binary_name,omitempty"`
	Error               string `json:"error,omitempty"`
}

type installStatusVersionFile struct {
	Path            string `json:"path"`
	Present         bool   `json:"present"`
	SkillVersion    string `json:"skill_version,omitempty"`
	MinRelayVersion string `json:"min_relay_version,omitempty"`
	Error           string `json:"error,omitempty"`
}

type installStatusState struct {
	Path                   string   `json:"path"`
	Present                bool     `json:"present"`
	Version                string   `json:"version,omitempty"`
	ClientsVerifiedVersion string   `json:"clients_verified_version,omitempty"`
	InstalledClients       []string `json:"installed_clients,omitempty"`
	Error                  string   `json:"error,omitempty"`
}

type installStatusClient struct {
	ID              string                    `json:"id"`
	Label           string                    `json:"label"`
	Configured      bool                      `json:"configured"`
	RuntimeDetected bool                      `json:"runtime_detected"`
	Attached        bool                      `json:"attached"`
	Ready           bool                      `json:"ready"`
	ActiveDrift     bool                      `json:"active_drift"`
	Roots           []installStatusClientRoot `json:"roots,omitempty"`
	Reason          string                    `json:"reason,omitempty"`
}

type installStatusClientRoot struct {
	Path                   string `json:"path"`
	Present                bool   `json:"present"`
	Kind                   string `json:"kind,omitempty"`
	Target                 string `json:"target,omitempty"`
	TransientBackupResidue bool   `json:"transient_backup_residue"`
}

type installStatusArtifact struct {
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	Classification string `json:"classification"`
	Detail         string `json:"detail,omitempty"`
}

func runStatus(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "json")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	report, err := buildInstallStatus(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			printHumanErr("%s", err)
			return 1
		}
		return 0
	}

	printHumanInfo("Version: %s", report.EffectiveVersion)
	printHumanInfo("Install root: %s", paths.InstallRoot)
	if len(report.ActiveDriftClients) > 0 {
		printHumanWarn("Active client drift: %s", strings.Join(report.ActiveDriftClients, ", "))
	}
	if len(report.InactiveArtifacts) > 0 {
		printHumanInfo("Inactive legacy/dev artifacts: %d", len(report.InactiveArtifacts))
	}
	return 0
}

func buildInstallStatus(paths runtimePaths) (installStatusReport, error) {
	state, statePresent, stateErr := readInstallStatusState(paths)
	report := installStatusReport{
		SchemaVersion:    1,
		EffectiveVersion: localVersion(paths),
		Binary: installStatusBinary{
			Version:      strings.TrimPrefix(Version, "v"),
			Display:      versionDisplay(paths),
			BuildChannel: BuildChannel,
			BuildStamp:   BuildStamp,
		},
		Bundle:      readInstallStatusBundle(paths),
		VersionFile: readInstallStatusVersionFile(paths),
		State: installStatusState{
			Path:    paths.StateFile,
			Present: statePresent,
			Error:   errorString(stateErr),
		},
	}
	if stateErr == nil {
		report.State.Version = state.Version
		report.State.ClientsVerifiedVersion = state.ClientsVerifiedVersion
		report.State.InstalledClients = append([]string{}, state.InstalledClients...)
		report.InstallSource = detectInstallSource(paths, state)
	} else {
		report.InstallSource = detectInstallSource(paths, defaultInstallState())
	}

	clients, err := loadClientRegistry(paths)
	if err != nil {
		return installStatusReport{}, err
	}
	active := map[string]bool{}
	for _, client := range clients {
		status := evaluateClientStatus(paths, state, client)
		if !status.Configured {
			continue
		}
		active[client.ID] = true
		clientReport := installStatusClient{
			ID:              status.ID,
			Label:           status.Label,
			Configured:      status.Configured,
			RuntimeDetected: status.RuntimeDetected,
			Attached:        status.Attached,
			Ready:           status.Ready,
			Reason:          status.Reason,
			Roots:           inspectStatusClientRoots(paths, client.ID),
		}
		for _, root := range clientReport.Roots {
			if root.TransientBackupResidue {
				clientReport.ActiveDrift = true
				break
			}
		}
		if clientReport.ActiveDrift {
			report.ActiveDriftClients = append(report.ActiveDriftClients, client.ID)
		}
		report.Clients = append(report.Clients, clientReport)
	}

	report.InactiveArtifacts = inactiveInstallArtifacts(paths, clients, active)
	report.ActiveDriftClients = normalizeClients(report.ActiveDriftClients)
	return report, nil
}

func readInstallStatusState(paths runtimePaths) (installState, bool, error) {
	state, err := loadState(paths)
	if err != nil {
		if isNotExist(err) {
			return defaultInstallState(), false, nil
		}
		return defaultInstallState(), true, err
	}
	return state, true, nil
}

func readInstallStatusBundle(paths runtimePaths) installStatusBundle {
	out := installStatusBundle{Path: paths.BundleFile}
	meta, err := loadBundleMetadata(paths)
	if err != nil {
		if isNotExist(err) {
			return out
		}
		out.Present = true
		out.Error = err.Error()
		return out
	}
	out.Present = true
	out.BundleFormatVersion = meta.BundleFormatVersion
	out.Version = strings.TrimPrefix(meta.Version, "v")
	out.OS = meta.OS
	out.Arch = meta.Arch
	out.BinaryName = meta.BinaryName
	return out
}

func readInstallStatusVersionFile(paths runtimePaths) installStatusVersionFile {
	out := installStatusVersionFile{Path: paths.VersionFile}
	v, err := readVersionJSON(paths.VersionFile)
	if err != nil {
		if isNotExist(err) {
			return out
		}
		out.Present = true
		out.Error = err.Error()
		return out
	}
	out.Present = true
	out.SkillVersion = strings.TrimPrefix(v.SkillVersion, "v")
	out.MinRelayVersion = strings.TrimPrefix(v.MinRelayVersion, "v")
	return out
}

func inspectStatusClientRoots(paths runtimePaths, client string) []installStatusClientRoot {
	roots := []installStatusClientRoot{}
	for _, root := range clientSkillTreeRoots(paths, client) {
		roots = append(roots, inspectStatusClientRoot(root))
	}
	return roots
}

func inspectStatusClientRoot(root string) installStatusClientRoot {
	status := installStatusClientRoot{Path: root}
	info, err := os.Lstat(root)
	if err != nil {
		return status
	}
	status.Present = true
	if info.Mode()&os.ModeSymlink != 0 {
		status.Kind = "symlink"
		if target, readErr := os.Readlink(root); readErr == nil {
			status.Target = target
		}
	} else if info.IsDir() {
		status.Kind = "directory"
	} else {
		status.Kind = "file"
	}
	status.TransientBackupResidue = pathHasTransientBackupResidue(root)
	return status
}

func inactiveInstallArtifacts(paths runtimePaths, clients []clientRegistryEntry, active map[string]bool) []installStatusArtifact {
	artifacts := []installStatusArtifact{}
	for _, artifact := range legacyInactiveArtifacts(paths) {
		if fileExists(artifact.Path) {
			artifacts = append(artifacts, artifact)
		}
	}
	for _, client := range clients {
		if active[client.ID] {
			continue
		}
		for _, root := range clientSkillTreeRoots(paths, client.ID) {
			if !pathHasTransientBackupResidue(root) {
				continue
			}
			artifacts = append(artifacts, installStatusArtifact{
				Kind:           "inactive_client_root",
				Path:           root,
				Classification: "inactive_legacy_or_dev_artifact",
				Detail:         fmt.Sprintf("%s root is not configured but still references a temporary update backup", client.Label),
			})
		}
	}
	return artifacts
}

func legacyInactiveArtifacts(paths runtimePaths) []installStatusArtifact {
	return []installStatusArtifact{
		{
			Kind:           "repo_dev_version_check_wrapper",
			Path:           filepath.Join(paths.ConfigDir, "version-check"),
			Classification: "inactive_legacy_or_dev_artifact",
			Detail:         "repo/dev compatibility wrapper; installed bundles use the ha-nova runtime directly",
		},
		{
			Kind:           "legacy_windows_check_update_wrapper",
			Path:           filepath.Join(paths.ConfigDir, "check-update.cmd"),
			Classification: "inactive_legacy_or_dev_artifact",
			Detail:         "legacy Windows compatibility wrapper; installed bundles use the ha-nova runtime directly",
		},
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
