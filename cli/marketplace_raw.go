package main

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
)

// The Marketplace's lifecycle as raw plan items
// (docs/work/2026-11-12-marketplace-lifecycle-spec.md): accepting its
// warning, installing or updating, and uninstalling are typed
// (plan_raw_tier.go rawTypedWS); everything else it offers stays in the
// Home Assistant UI.

// checkRawMarketplace: each lifecycle request in exactly the shape the
// skill sends. An install never picks a version or replaces an
// integration built into Home Assistant (confirm_replace_built_in): both
// stay in the Marketplace itself.
func checkRawMarketplace(wsType string, message map[string]any) error {
	want := []string{"repository", "type"}
	if wsType == "marketplace/warning/accept" {
		want = []string{"type"}
	}
	repository, _ := message["repository"].(string)
	if !slices.Equal(slices.Sorted(maps.Keys(message)), want) || len(want) == 2 && repository == "" {
		return rawRefusal(wsType+" with fields other than "+strings.Join(want, " and "), "send only those fields; a version, or replacing a built-in integration, is picked in the Home Assistant UI (Settings > Marketplace)")
	}
	return nil
}

// marketplaceSupervisor: HA NOVA's Home Assistant user is the Supervisor
// system user, the NOVA App's, which every App with Home Assistant access
// shares (Core hassio; Supervisor api/proxy.py). Only that user's warning
// is accepted from here (maintainer decision D2): a person's own account
// accepts it in the Marketplace itself. Read live; a failed read is an
// error, never a yes.
func marketplaceSupervisor(s *relaySession) (bool, error) {
	var users []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		SystemGenerated bool   `json:"system_generated"`
	}
	var current struct {
		ID string `json:"id"`
	}
	data, err := rawRead(s, "ws", map[string]any{"type": "auth/current_user"})
	if err == nil {
		err = json.Unmarshal(data, &current)
	}
	if err == nil {
		data, err = rawRead(s, "ws", map[string]any{"type": "config/auth/list"})
	}
	if err == nil {
		err = json.Unmarshal(data, &users)
	}
	if err == nil && current.ID == "" {
		err = errors.New("HA NOVA's own account came back empty")
	}
	if err != nil {
		return false, errors.New("cannot read which Home Assistant account HA NOVA uses: " + err.Error())
	}
	for _, user := range users {
		if user.ID == current.ID {
			return user.SystemGenerated && user.Name == "Supervisor", nil
		}
	}
	return false, nil
}

// rawMarketplaceUser refuses accepting the warning for any account but
// the Supervisor user (marketplaceSupervisor).
func rawMarketplaceUser(s *relaySession) (refusal, err error) {
	supervisor, err := marketplaceSupervisor(s)
	if err != nil || supervisor {
		return nil, err
	}
	return rawRefusal("accepting the Marketplace warning for HA NOVA's account, which is a person's account, not the NOVA App's Supervisor user", "sign in to Home Assistant with that account and accept it under Settings > Marketplace"), nil
}
