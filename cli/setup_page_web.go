package main

import (
	"context"
	"slices"
	"time"
)

// Claude or ChatGPT in the browser (the No-AI-app screen's "web"): what chat
// apps need is checked with the owner's sign-in, before the App is touched
// or this computer pairs, so the page never pairs into a dead end.

const chatAppsGuideURL = haNovaRepositoryURL + "/blob/main/docs/reference/chat-apps.md"

// setupWebNotReady: chat apps can't work with this Home Assistant yet; msg
// says why. never: nothing to set up here can change that (no Supervisor).
type setupWebNotReady struct {
	msg   *setupPageMsg
	never bool
}

func (e *setupWebNotReady) Error() string {
	return "chat apps can't work with this Home Assistant yet (" + e.msg.Key + ")"
}

// pageWebCheck reads, on the owner's sign-in socket, what chat apps need:
// Home Assistant OS or Supervised, 2026.10 or newer, and an internet address
// by the chat-app row's rule (chatAppsBase: Link with remote access, else a
// public HTTPS external URL). Link is read only where its integration runs;
// a failed read there fails the sign-in, never reads as "no address".
func pageWebCheck(ctx context.Context, socket haWSCaller) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second) // as setupLinkRemoteOrigin; prepare's own bound is the App install's
	defer cancel()
	var config struct {
		Version     string   `json:"version"`
		ExternalURL string   `json:"external_url"`
		Components  []string `json:"components"`
	}
	if err := socket.Call(ctx, "get_config", nil, &config); err != nil {
		return err
	}
	switch {
	case !slices.Contains(config.Components, "hassio"):
		return &setupWebNotReady{msg: setupPageText("setup.chat.problem_no_apps"), never: true}
	case !chatAppsHAVersionOK(config.Version):
		return &setupWebNotReady{msg: setupPageText("setup.chat.problem_old")}
	}
	link := ""
	if slices.Contains(config.Components, "cloud") {
		var status HACloudStatus
		if err := socket.Call(ctx, "cloud/status", nil, &status); err != nil {
			return err
		}
		if origin, err := cloudLinkOrigin(ctx, status); err == nil { // not ready: no Link address, as setupLinkRemoteOrigin
			link = origin.InputOrigin
		}
	}
	if chatAppsBase(link, config.ExternalURL) == "" {
		// The App may not be installed yet: no "open NOVA" here.
		return &setupWebNotReady{msg: setupPageText("setup.chat.need_address_web")}
	}
	return nil
}

// pageUseThisComputer: "Use HA NOVA on this computer instead" after the
// check above. Back to the No-AI-app screen, where the downloads and Check
// again are; an AI app found there is set up as on a first run.
func (f *setupFlow) pageUseThisComputer() setupStepResult {
	f.noClient = false
	f.page.update(func(v *setupPageState) { v.Web, v.Problem, v.Fix, v.Links = false, nil, "", nil })
	if result := f.pageAskForClient(true); result.done || f.noClient {
		return result
	}
	var err error
	if f.selectedClients, f.skippedClients, err = resolveSetupClientsWithChoices(f.choices, f.target); err != nil {
		return f.failErr(err)
	}
	return setupGoOn
}
