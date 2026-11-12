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
// finds nothing that does (relayGuidedCallAllowed).

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
func checkHomeAssistantForRelayUpdate(paths runtimePaths, route *relayUpdateRoute, candidate relayUpdateCandidate, out io.Writer) (relayUpdateCandidate, bool) {
	enough := func(c relayUpdateCandidate) bool {
		return c.updateAvailable() && relayVersionMeetsUpdateGate(paths, c.LatestVersion, "")
	}
	if candidate.EntityID == "" || candidate.InProgress || enough(candidate) {
		return candidate, false
	}
	body, _ := json.Marshal(map[string]string{"entity_id": candidate.EntityID})
	checked, _ := runSetupSpinnerWithResult(out, "Checking Home Assistant for a newer NOVA App...", setupSpinnerDebounce, func() (relayUpdateCandidate, error) {
		refresh := planCallRequest{Service: "homeassistant.update_entity", EntityIDs: []string{candidate.EntityID}, Data: map[string]any{}}
		if !relayGuidedCallAllowed(paths, route, refresh, tierOrdinary) {
			return candidate, nil
		}
		reply, err := route.core("POST", "/api/services/homeassistant/update_entity", body)
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
			latest := relayUpdateEntityLatest(route, candidate.EntityID)
			if latest == "" || latest == seen {
				continue
			}
			current, _ := route.candidate()
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

// guidedRating is what `ha-nova call` would plan for one of the guided
// update's own calls (computeCallPlan: call_service listeners, the
// targets' consumers, ...), read over the guided update's route; err: the
// check itself failed, so nothing goes out.
type guidedRating struct {
	level    tierLevel
	complete bool // a complete scan, its targets frozen
	err      error
}

// allows: planned at highest or below, with a complete scan and its
// targets frozen. A failed or cut read never passes.
func (r guidedRating) allows(highest tierLevel) bool {
	return r.err == nil && r.complete && r.level <= highest
}

// typed: it needs a typed confirmation (relay_guided_update_typed.go).
func (r guidedRating) typed() bool { return r.err == nil && r.level == tierTyped }

// why: the check's own words when it failed, else why it isn't complete.
func (r guidedRating) why() string {
	if r.err != nil {
		return r.err.Error()
	}
	return "some of what reacts to it couldn't be read"
}

func rateGuidedCall(paths runtimePaths, route *relayUpdateRoute, req planCallRequest) guidedRating {
	if refusal := staticRefusal(req); refusal != nil {
		return guidedRating{err: refuse(&verbEnvelope{}, *refusal)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), relaySessionOverallTimeout)
	s := &relaySession{paths: paths, cfg: route.cfg, transport: route.relayTransportSelection,
		server: activeServerProfile(), ctx: ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{}}
	defer s.close()
	env := verbEnvelope{SchemaVersion: 1, Verb: "call", Server: s.server}
	e, plan, err := computeCallPlan(s, &env, req)
	if err != nil {
		return guidedRating{err: err}
	}
	return guidedRating{level: e.level, complete: e.complete && !frozeNothing(req, plan.Action)}
}

// relayGuidedCallAllowed: rateGuidedCall allows req at highest or below.
func relayGuidedCallAllowed(paths runtimePaths, route *relayUpdateRoute, req planCallRequest, highest tierLevel) bool {
	return rateGuidedCall(paths, route, req).allows(highest)
}

// relayUpdateEntityLatest reads one update entity's latest_version when it
// shows an update, else "".
func relayUpdateEntityLatest(route *relayUpdateRoute, entityID string) string {
	reply, err := route.core("GET", "/api/states/"+entityID, nil)
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
