package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
)

const cloudReleaseEvidenceSchema = 1

// The matching private key exists only as the protected GitHub production
// environment secret HA_NOVA_CLOUD_RELEASE_SIGNING_KEY_PEM. Rotation requires
// one reviewed source change plus an atomic secret replacement before an RC.
var cloudReleaseEvidencePublicKey = ed25519.PublicKey{
	0x87, 0x38, 0x10, 0x7a, 0x26, 0x0b, 0xc2, 0x92,
	0x5d, 0x80, 0xbe, 0x76, 0x21, 0xf7, 0x2c, 0x23,
	0x34, 0xdf, 0x0a, 0xcb, 0x5c, 0x7e, 0xa6, 0xd1,
	0xba, 0x8c, 0x96, 0x9a, 0x63, 0xe4, 0xc2, 0xb4,
}

var cloudReleaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var cloudReleaseTreePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type cloudReleaseSignedPayload struct {
	Schema        int      `json:"schema"`
	Version       string   `json:"version"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	BinaryName    string   `json:"binary_name"`
	BinarySHA256  string   `json:"binary_sha256"`
	SourceTreeSHA string   `json:"source_tree_sha"`
	Platforms     []string `json:"platforms"`
}

func verifyCloudReleaseProvenance(
	bundle bundleMetadata,
	metadata versionJSON,
	executablePath string,
) bool {
	evidence := bundle.CloudRelease
	if evidence == nil ||
		evidence.Schema != cloudReleaseEvidenceSchema ||
		len(cloudReleaseEvidencePublicKey) != ed25519.PublicKeySize ||
		!cloudReleaseSHA256Pattern.MatchString(evidence.BinarySHA256) ||
		!cloudReleaseTreePattern.MatchString(evidence.SourceTreeSHA) {
		return false
	}

	executable, err := os.ReadFile(executablePath)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(executable)
	if hex.EncodeToString(digest[:]) != evidence.BinarySHA256 {
		return false
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(evidence.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	payload, err := json.Marshal(cloudReleaseSignedPayload{
		Schema:        evidence.Schema,
		Version:       bundle.Version,
		OS:            bundle.OS,
		Arch:          bundle.Arch,
		BinaryName:    bundle.BinaryName,
		BinarySHA256:  evidence.BinarySHA256,
		SourceTreeSHA: evidence.SourceTreeSHA,
		Platforms:     metadata.CloudRemotePlatforms,
	})
	return err == nil &&
		ed25519.Verify(cloudReleaseEvidencePublicKey, payload, signature)
}
