package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Stage 2 of the guided relay update
// (docs/work/2026-07-12-relay-guided-update-spec.md): after the
// relay-outdated warning on an interactive terminal, `ha-nova update` and
// `ha-nova doctor` offer to install the App update right there — ask first,
// never automatic. Everything here is client-side and generic HA transport
// through the existing /core proxy; the relay stays dumb.

// relayUpdateEntityTitle is the App name the update entity carries
// (nova/config.yaml `name:`). Resolution matches on it exactly — on zero or
// several matches the flow prints the manual path instead of guessing.
const relayUpdateEntityTitle = "NOVA Relay"

// Poll pacing is a variable so tests can shrink the restart wait.
var (
	relayUpdatePollInterval = 5 * time.Second
	relayUpdatePollTimeout  = 3 * time.Minute
)

const relayUpdateManualPath = "Manual path: open Home Assistant > Settings > Add-ons > NOVA Relay and install the update there; a standalone container needs a manual image pull."

// maybeOfferGuidedRelayUpdate follows a printed relay-outdated notice and
// reports whether the relay ended up updated AND verified — doctor uses that
// to not fail a run whose only problem the user just fixed. It is
// deliberately best-effort: a non-TTY session, a "no", a missing update
// entity (standalone container), or any transport problem falls back to the
// manual path without touching the caller's exit code.
func maybeOfferGuidedRelayUpdate(paths runtimePaths, notice humanNotice) bool {
	// Both ends must be a terminal: with stdout redirected the question would
	// land in a file while the command silently blocks on stdin.
	if notice.kind != humanNoticeKindRelayOutdated || !isInteractiveTTY() || !stdoutIsInteractiveTTY() {
		return false
	}
	cfg, err := loadConfig(paths)
	if err != nil || cfg.RelayBaseURL == "" {
		return false
	}
	token, err := readRelayAuthTokenForDoctor()
	if err != nil || token == "" {
		return false
	}
	return runGuidedRelayUpdate(paths, cfg, token, bufio.NewReader(os.Stdin), os.Stdout)
}

func runGuidedRelayUpdate(paths runtimePaths, cfg config, token string, in *bufio.Reader, out io.Writer) bool {
	yes, err := promptWizardYesNoFromReader(in, out, "Install the relay update in Home Assistant now?", true)
	if err != nil || !yes {
		return false
	}
	entityID, reason := resolveRelayUpdateEntity(cfg, token)
	if entityID == "" {
		fmt.Fprintf(out, "Cannot start the App update from here: %s\n%s\n", reason, relayUpdateManualPath)
		return false
	}
	fmt.Fprintf(out, "Installing the NOVA Relay App update (%s) with a partial backup — the relay restarts during the install.\n", entityID)
	// The relay dies mid-call when the install lands, so a dropped response
	// is the EXPECTED shape of success here; polling decides the outcome.
	// An ok:false envelope is a relay-side transport problem (the relay's
	// upstream client times out at 10s — a backup easily exceeds that) and
	// polls too. Only Home Assistant itself answering >= 400 (ok:true) is an
	// explicit rejection that skips the three-minute wait.
	body, err := relayCoreRequest(cfg, token, "POST", "/api/services/update/install",
		[]byte(fmt.Sprintf(`{"entity_id":%q,"backup":true}`, entityID)))
	if err == nil {
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Status int `json:"status"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.OK && envelope.Data.Status >= 400 {
			fmt.Fprintf(out, "Home Assistant rejected the install call.\n%s\n", relayUpdateManualPath)
			return false
		}
	}
	version, ok := waitForRelayFloor(paths, cfg, token)
	if !ok {
		fmt.Fprintf(out, "The relay did not report a new version within %s.\n%s\n", relayUpdatePollTimeout, relayUpdateManualPath)
		return false
	}
	fmt.Fprintf(out, "Relay updated and verified: v%s is running.\n", version)
	return true
}

// resolveRelayUpdateEntity finds the NOVA Relay App's update.* entity via
// GET /api/states. It returns the entity id, or "" plus the human reason
// (no entity → container/manual install; several → ambiguous, never guess).
func resolveRelayUpdateEntity(cfg config, token string) (string, string) {
	body, err := relayCoreRequest(cfg, token, "GET", "/api/states", nil)
	if err != nil {
		return "", fmt.Sprintf("could not read Home Assistant states (%s)", err)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK || envelope.Data.Status != http.StatusOK {
		return "", "could not read Home Assistant states"
	}
	var states []struct {
		EntityID   string `json:"entity_id"`
		Attributes struct {
			Title string `json:"title"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(envelope.Data.Body, &states); err != nil {
		return "", "could not read Home Assistant states"
	}
	var matches []string
	for _, s := range states {
		if strings.HasPrefix(s.EntityID, "update.") && s.Attributes.Title == relayUpdateEntityTitle {
			matches = append(matches, s.EntityID)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], ""
	case 0:
		return "", "no NOVA Relay App update entity found (standalone container, or the App is not installed)"
	default:
		return "", fmt.Sprintf("several update entities carry the title %q — not guessing", relayUpdateEntityTitle)
	}
}

// waitForRelayFloor polls GET /health until the reported version satisfies
// min_relay_version, then returns it. Connection errors are the normal
// restart window and just keep the loop going.
func waitForRelayFloor(paths runtimePaths, cfg config, token string) (string, bool) {
	deadline := time.Now().Add(relayUpdatePollTimeout)
	for {
		body, err := fetchRelayHealth(cfg.RelayBaseURL, token)
		if err == nil {
			version := parseRelayHealthVersion(body)
			if version != "" && checkRelayVersionValue(paths, version).empty() {
				return version, true
			}
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(relayUpdatePollInterval)
	}
}

// relayCoreRequest posts one request through the relay's /core proxy and
// returns the raw envelope body.
func relayCoreRequest(cfg config, token, method, path string, requestBody []byte) ([]byte, error) {
	payload := []byte(fmt.Sprintf(`{"method":%q,"path":%q`, method, path))
	if len(requestBody) > 0 {
		payload = append(payload, []byte(`,"body":`)...)
		payload = append(payload, requestBody...)
	}
	payload = append(payload, '}')
	req, err := http.NewRequest("POST", strings.TrimRight(cfg.RelayBaseURL, "/")+"/core", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readAllLimited(resp.Body, maxRelayResponseBytes)
}
