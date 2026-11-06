package main

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// The Supervisor reloads its App store only every few hours, so a NOVA App
// published since then has no update in Home Assistant yet. Updating the
// App's update entity (homeassistant.update_entity, as "Check for updates"
// in Home Assistant does) makes the Supervisor integration reload the store
// first; then the entity shows the new version. It installs nothing and
// goes through the same /core proxy as the install. It is still a service
// call automations can react to, so it goes out only when the tier engine
// finds nothing that does (relayStoreCheckOrdinary).

// Variables so tests can shrink the wait.
var (
	relayStoreCheckPollInterval = 3 * time.Second
	relayStoreCheckTimeout      = time.Minute
)

// checkHomeAssistantForRelayUpdate runs when Home Assistant offers no App
// update, or only one below what this HA NOVA needs. It returns the
// candidate as Home Assistant shows it afterwards, and whether it checked:
// it asked, or found something that reacts to asking and left it to the
// person (relayUpdateCheckManualPath).
func checkHomeAssistantForRelayUpdate(paths runtimePaths, cfg config, token string, candidate relayUpdateCandidate, out io.Writer) (relayUpdateCandidate, bool) {
	enough := func(c relayUpdateCandidate) bool {
		return c.updateAvailable() && relayVersionMeetsUpdateGate(paths, c.LatestVersion, "")
	}
	if candidate.EntityID == "" || candidate.InProgress || enough(candidate) {
		return candidate, false
	}
	body, _ := json.Marshal(map[string]string{"entity_id": candidate.EntityID})
	checked, _ := runSetupSpinnerWithResult(out, "Checking Home Assistant for a newer NOVA App...", setupSpinnerDebounce, func() (relayUpdateCandidate, error) {
		if !relayStoreCheckOrdinary(paths, cfg, token, candidate.EntityID) {
			return candidate, nil
		}
		reply, err := relayCoreRequest(cfg, token, "POST", "/api/services/homeassistant/update_entity", body)
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Status int `json:"status"`
			} `json:"data"`
		}
		// Only Home Assistant itself turning it down ends the check early;
		// a dropped or slow reply may still have started the reload.
		if err == nil && json.Unmarshal(reply, &envelope) == nil && envelope.OK && envelope.Data.Status >= 400 {
			return candidate, nil
		}
		// Poll only the App's own state; the full registry-proven read runs
		// once that state shows a new version.
		best, seen := candidate, ""
		for deadline := time.Now().Add(relayStoreCheckTimeout); time.Now().Before(deadline); {
			time.Sleep(relayStoreCheckPollInterval)
			latest := relayUpdateEntityLatest(cfg, token, candidate.EntityID)
			if latest == "" || latest == seen {
				continue
			}
			current, _ := resolveRelayUpdateCandidate(cfg, token)
			if current.EntityID != candidate.EntityID {
				continue
			}
			seen = latest
			if !current.updateAvailable() {
				continue
			}
			best = current
			if enough(current) {
				break
			}
		}
		return best, nil
	})
	return checked, true
}

// relayStoreCheckOrdinary: true only when `ha-nova call
// homeassistant.update_entity <entity>` would send that call without asking
// (computeCallPlan: call_service listeners, the entity's consumers, ...).
// The session reads over the guided update's own server and credential; a
// failed or cut read is never ordinary.
func relayStoreCheckOrdinary(paths runtimePaths, cfg config, token, entityID string) bool {
	base, client, credential, err := functionalEndpoint(cfg, token)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), relaySessionOverallTimeout)
	s := &relaySession{paths: paths, cfg: cfg, transport: relayTransportSelection{BaseURL: base, Client: client, Credential: credential, Via: relayViaLocal},
		server: activeServerProfile(), ctx: ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{}}
	defer s.close()
	req := planCallRequest{Service: "homeassistant.update_entity", EntityIDs: []string{entityID}, Data: map[string]any{}}
	if staticRefusal(req) != nil {
		return false
	}
	env := verbEnvelope{SchemaVersion: 1, Verb: "call", Server: s.server}
	e, plan, err := computeCallPlan(s, &env, req)
	return err == nil && e.level == tierOrdinary && e.complete && !frozeNothing(req, plan.Action)
}

// relayUpdateEntityLatest reads one update entity's latest_version when it
// shows an update, else "".
func relayUpdateEntityLatest(cfg config, token, entityID string) string {
	reply, err := relayCoreRequest(cfg, token, "GET", "/api/states/"+entityID, nil)
	if err != nil {
		return ""
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int `json:"status"`
			Body   struct {
				State      string `json:"state"`
				Attributes struct {
					LatestVersion string `json:"latest_version"`
				} `json:"attributes"`
			} `json:"body"`
		} `json:"data"`
	}
	if json.Unmarshal(reply, &envelope) != nil || !envelope.OK || envelope.Data.Status != 200 || envelope.Data.Body.State != "on" {
		return ""
	}
	return envelope.Data.Body.Attributes.LatestVersion
}
