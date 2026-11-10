package main

import "strings"

// Home Assistant 2026.11+ installs a Marketplace update only for a user who
// accepted the Marketplace warning (marketplace/update.py async_install;
// update.skip and update.clear_skipped run without that check). The Relay's
// own user can't open that warning, so HA NOVA refuses the install before
// sending it instead of letting Home Assistant answer HTTP 500.
const reasonMarketplaceWarning = "MARKETPLACE_WARNING"

// marketplaceWarningError: Home Assistant's English text for that HTTP 500
// (an exception's message is always the English translation).
const marketplaceWarningError = "read the warning first"

const marketplaceNext = "Tell the user exactly what say says, nothing more: nothing was installed. Never retry it, batch it, or install it another way."

// marketplaceRefusal: an update.install on a Marketplace update entity is
// refused unless marketplace/info says the Relay's user accepted the
// warning; a failed read refuses too.
func (e *tierEngine) marketplaceRefusal(env *verbEnvelope, req planCallRequest, targets []verbTarget) *verbFailure {
	if req.Service != "update.install" {
		return nil
	}
	for _, t := range targets {
		if !strings.HasPrefix(t.EntityID, "update.") || e.reg[t.EntityID].pl != "marketplace" {
			continue
		}
		var info struct {
			WarningAccepted *bool `json:"warning_accepted"`
		}
		why := "the Relay's Home Assistant user has not accepted the Marketplace warning"
		switch err := e.s.wsDecode(map[string]any{"type": "marketplace/info"}, &info, "the Marketplace info"); {
		case err != nil:
			why = "HA NOVA couldn't read marketplace/info: " + err.Error()
		case info.WarningAccepted == nil:
			why = "marketplace/info has no warning_accepted"
		case *info.WarningAccepted:
			return nil
		}
		return marketplaceRefused(env, e.s.language(), t.EntityID, why)
	}
	return nil
}

func marketplaceRefused(env *verbEnvelope, lang, entityID, why string) *verbFailure {
	f := refuse(env, tierReason{Code: reasonMarketplaceWarning, EntityID: entityID, Detail: why})
	f.message = "refused (" + reasonMarketplaceWarning + "): " + why + "; nothing was installed"
	f.say, f.next = confirmT(lang, "say.marketplace_update"), marketplaceNext
	return f
}

// marketplaceRejected: Home Assistant's own HTTP 500 for that warning, when
// an install reached it anyway (accepted at plan time, withdrawn since).
// Only a single target proves nothing ran: Home Assistant installs several
// at once and reports the first failure, so those stay unverified.
func marketplaceRejected(s *relaySession, env *verbEnvelope, action planAction, result serviceResult) *verbFailure {
	if action.Service != "update.install" || len(action.EntityIDs) != 1 || result.status != 500 || !strings.Contains(string(result.body), marketplaceWarningError) {
		return nil
	}
	return marketplaceRefused(env, s.language(), "", "Home Assistant refused the install before it ran (HTTP 500: "+truncate(string(result.body), 300)+")")
}
