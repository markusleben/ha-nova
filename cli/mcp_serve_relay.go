package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"
)

// `ha-nova serve --relay-file`: the NOVA App's internal device credential
// (docs/work/2026-10-19-chat-bridge-contract.md, "App side"). The App mints
// an ordinary device record in its own Relay registry and writes this file
// (0600); the bridge then reaches the Relay's pinned TLS port with it. No
// config.json, keyring, pairing code, or Cloud route is involved.

type bridgeRelayFile struct {
	RelayURL   string `json:"relay_url"`
	SpkiPin    string `json:"spki_pin"`
	Credential string `json:"credential"`
}

var (
	bridgeSpkiPin    = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)                                  // sha256, base64url
	bridgeCredential = regexp.MustCompile(`^hanova-dev-v1\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}$`) // nova device-credential.ts
)

// bridgeRelay, when set, is the only Relay this process talks to
// (loadRuntimeConfig returns it for every server selection).
var bridgeRelay *runtimeConfig

func loadBridgeRelay(file string) (*runtimeConfig, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the relay file: %w", err)
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("the relay file must be a regular file readable by its owner only (chmod 600)")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the relay file: %w", err)
	}
	var relay bridgeRelayFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&relay); err != nil {
		return nil, fmt.Errorf("the relay file is not valid: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("the relay file is not valid: trailing data")
	}
	u, err := url.Parse(relay.RelayURL)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "":
		return nil, errors.New("the relay file needs relay_url https://host:port")
	case !bridgeSpkiPin.MatchString(relay.SpkiPin):
		return nil, errors.New("the relay file needs the Relay's spki_pin")
	case !bridgeCredential.MatchString(relay.Credential):
		return nil, errors.New("the relay file needs a device credential")
	}
	return &runtimeConfig{RelayBaseURL: relay.RelayURL, RelaySecureBaseURL: relay.RelayURL, RelaySpkiPin: relay.SpkiPin,
		RoutePolicy: routePolicyLocal, bridgeCredential: relay.Credential}, nil
}
