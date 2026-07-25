package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCloudReleaseSignerMatchesRuntimeVerifier(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateDER,
	})
	binaryPath := filepath.Join(t.TempDir(), publicBinaryName())
	if err := os.WriteFile(binaryPath, []byte("official artifact"), 0o700); err != nil {
		t.Fatal(err)
	}
	const tree = "1234567890abcdef1234567890abcdef12345678"
	command := exec.Command(
		"node",
		"../scripts/release/sign-cloud-release-evidence.mjs",
		"2.0.0-rc1",
		bundlePlatformOS(),
		bundlePlatformArch(),
		publicBinaryName(),
		binaryPath,
		tree,
		runtime.GOOS,
	)
	command.Env = append(
		os.Environ(),
		"HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM="+string(privatePEM),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("signer failed: %v\n%s", err, output)
	}
	var evidence cloudReleaseBundleEvidence
	if err := json.Unmarshal(output, &evidence); err != nil {
		t.Fatal(err)
	}
	previousKey := cloudReleaseEvidencePublicKey
	cloudReleaseEvidencePublicKey = publicKey
	defer func() {
		cloudReleaseEvidencePublicKey = previousKey
	}()
	if !verifyCloudReleaseProvenance(
		bundleMetadata{
			Version:      "2.0.0-rc1",
			OS:           bundlePlatformOS(),
			Arch:         bundlePlatformArch(),
			BinaryName:   publicBinaryName(),
			CloudRelease: &evidence,
		},
		versionJSON{CloudRemotePlatforms: []string{runtime.GOOS}},
		binaryPath,
	) {
		t.Fatal("runtime rejected evidence emitted by the release signer")
	}
}

func TestCloudRemoteFeatureGateRejectsAmbientBundleForRawExecutable(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	metadata := enabledCloudFeatureMetadata("2.0.0")
	paths := writeCloudFeatureReleaseBundle(t, "2.0.0", "2.0.0", metadata)
	rawExecutable := filepath.Join(t.TempDir(), publicBinaryName())
	if err := os.WriteFile(rawExecutable, []byte("raw runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	executablePathForInstallSource = func() (string, error) {
		return rawExecutable, nil
	}

	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("ambient enabled bundle enabled Cloud Remote for a raw executable")
	}
}

func TestCloudRemoteFeatureGateRejectsReleaseVersionMismatch(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	paths := writeCloudFeatureReleaseBundle(
		t,
		"1.0.0",
		"1.0.0",
		enabledCloudFeatureMetadata("2.0.0"),
	)
	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("newer ambient release metadata enabled an older binary")
	}
}

func TestCloudRemoteFeatureGateRejectsBundleVersionMismatch(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	paths := writeCloudFeatureReleaseBundle(
		t,
		"2.0.0",
		"1.0.0",
		enabledCloudFeatureMetadata("2.0.0"),
	)
	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("mismatched bundle metadata enabled Cloud Remote")
	}
}

func TestCloudRemoteFeatureGateRejectsUnsignedAndTamperedRuntime(t *testing.T) {
	for _, test := range []struct {
		name   string
		tamper func(*bundleMetadata)
	}{
		{
			name: "missing evidence",
			tamper: func(bundle *bundleMetadata) {
				bundle.CloudRelease = nil
			},
		},
		{
			name: "modified signed platform",
			tamper: func(bundle *bundleMetadata) {
				bundle.OS = "tampered"
			},
		},
		{
			name: "modified signature",
			tamper: func(bundle *bundleMetadata) {
				bundle.CloudRelease.Signature = base64.StdEncoding.EncodeToString(
					make([]byte, ed25519.SignatureSize),
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			restore := setCloudFeatureTestBuild(t, false)
			defer restore()
			paths := writeCloudFeatureReleaseBundle(
				t,
				"2.0.0",
				"2.0.0",
				enabledCloudFeatureMetadata("2.0.0"),
			)
			data, err := os.ReadFile(paths.BundleFile)
			if err != nil {
				t.Fatal(err)
			}
			var bundle bundleMetadata
			if err := json.Unmarshal(data, &bundle); err != nil {
				t.Fatal(err)
			}
			test.tamper(&bundle)
			data, err = json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.BundleFile, data, 0o600); err != nil {
				t.Fatal(err)
			}
			configureCloudRemoteFeature(paths)
			if cloudRemoteFeatureAvailable() {
				t.Fatal("invalid release provenance enabled Cloud Remote")
			}
		})
	}
}

func TestCloudRemoteFeatureGateRejectsUnprovisionedReleaseKey(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()
	paths := writeCloudFeatureReleaseBundle(
		t,
		"2.0.0",
		"2.0.0",
		enabledCloudFeatureMetadata("2.0.0"),
	)
	cloudReleaseEvidencePublicKey = nil

	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("unprovisioned release key enabled Cloud Remote")
	}
}

func TestCloudRemoteFeatureGateAcceptsExactInstalledReleaseCandidateRuntime(
	t *testing.T,
) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	paths := writeCloudFeatureReleaseBundle(
		t,
		"2.0.0-rc1",
		"2.0.0-rc1",
		enabledCloudFeatureMetadata("2.0.0"),
	)
	configureCloudRemoteFeature(paths)
	if cloudRemotePlatformSupported(runtime.GOOS) != cloudRemoteFeatureAvailable() {
		t.Fatalf(
			"exact installed release-candidate availability for %s = %v",
			runtime.GOOS,
			cloudRemoteFeatureAvailable(),
		)
	}
}

func TestCloudRemoteFeatureGateRejectsInvalidReleaseRuntimeVersionBindings(
	t *testing.T,
) {
	tests := []struct {
		name            string
		binaryVersion   string
		bundleVersion   string
		metadataVersion string
	}{
		{"RC bundle mismatch", "2.0.0-rc1", "2.0.0-rc2", "2.0.0"},
		{"RC base mismatch", "2.0.0-rc1", "2.0.0-rc1", "2.0.1"},
		{"snapshot", "2.0.0-snapshot", "2.0.0-snapshot", "2.0.0"},
		{"prerelease", "2.0.0-beta1", "2.0.0-beta1", "2.0.0"},
		{"invalid RC zero", "2.0.0-rc0", "2.0.0-rc0", "2.0.0"},
		{"version prefix", "v2.0.0", "v2.0.0", "2.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := setCloudFeatureTestBuild(t, false)
			defer restore()

			paths := writeCloudFeatureReleaseBundle(
				t,
				test.binaryVersion,
				test.bundleVersion,
				enabledCloudFeatureMetadata(test.metadataVersion),
			)
			configureCloudRemoteFeature(paths)
			if cloudRemoteFeatureAvailable() {
				t.Fatal("invalid release version binding enabled Cloud Remote")
			}
		})
	}
}

func TestCloudRemoteFeatureGateAcceptsExactInstalledReleaseRuntime(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	paths := writeCloudFeatureReleaseBundle(
		t,
		"2.0.0",
		"2.0.0",
		enabledCloudFeatureMetadata("2.0.0"),
	)
	configureCloudRemoteFeature(paths)
	if cloudRemotePlatformSupported(runtime.GOOS) != cloudRemoteFeatureAvailable() {
		t.Fatalf(
			"exact installed release availability for %s = %v",
			runtime.GOOS,
			cloudRemoteFeatureAvailable(),
		)
	}
}

func enabledCloudFeatureMetadata(version string) string {
	return fmt.Sprintf(
		`{"skill_version":%q,"min_relay_version":"1.0.0","cloud_remote_enabled":true,"cloud_remote_platforms":[%q]}`,
		version,
		runtime.GOOS,
	)
}

func writeCloudFeatureReleaseBundle(
	t *testing.T,
	binaryVersion string,
	bundleVersion string,
	versionMetadata string,
) runtimePaths {
	t.Helper()
	installRoot := t.TempDir()
	runtimePath := filepath.Join(installRoot, publicBinaryName())
	runtimeBytes := []byte("installed runtime")
	if err := os.WriteFile(runtimePath, runtimeBytes, 0o700); err != nil {
		t.Fatal(err)
	}
	versionPath := filepath.Join(installRoot, "version.json")
	if err := os.WriteFile(versionPath, []byte(versionMetadata), 0o600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(installRoot, "bundle.json")
	var metadata versionJSON
	if err := json.Unmarshal([]byte(versionMetadata), &metadata); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousPublicKey := cloudReleaseEvidencePublicKey
	cloudReleaseEvidencePublicKey = publicKey
	t.Cleanup(func() {
		cloudReleaseEvidencePublicKey = previousPublicKey
	})
	digest := sha256.Sum256(runtimeBytes)
	evidence := cloudReleaseBundleEvidence{
		Schema:        cloudReleaseEvidenceSchema,
		SourceTreeSHA: "1a2b3c4d5e6f7890123456789012345678901234",
		BinarySHA256:  hex.EncodeToString(digest[:]),
	}
	payload, err := json.Marshal(cloudReleaseSignedPayload{
		Schema:        evidence.Schema,
		Version:       bundleVersion,
		OS:            bundlePlatformOS(),
		Arch:          bundlePlatformArch(),
		BinaryName:    publicBinaryName(),
		BinarySHA256:  evidence.BinarySHA256,
		SourceTreeSHA: evidence.SourceTreeSHA,
		Platforms:     metadata.CloudRemotePlatforms,
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence.Signature = base64.StdEncoding.EncodeToString(
		ed25519.Sign(privateKey, payload),
	)
	bundle := bundleMetadata{
		BundleFormatVersion: bundleFormatVersion,
		Version:             bundleVersion,
		OS:                  bundlePlatformOS(),
		Arch:                bundlePlatformArch(),
		BinaryName:          publicBinaryName(),
		CloudRelease:        &evidence,
	}
	bundleBytes, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePath, bundleBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	Version = binaryVersion
	executablePathForInstallSource = func() (string, error) {
		return runtimePath, nil
	}
	return runtimePaths{
		InstallRoot: installRoot,
		VersionFile: versionPath,
		BundleFile:  bundlePath,
	}
}
