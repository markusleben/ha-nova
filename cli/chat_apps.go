package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Chat apps on the computer side (docs/work/2026-10-18-onboarding-0.27-spec.md
// §11.6, contract docs/work/2026-10-19-chat-bridge-contract.md): the NOVA
// App's owner-only switch, called with the owner's own ingress session from
// this run's Home Assistant sign-in. Setup's step 3 row, its text-path
// question, and `ha-nova chat-apps` all use it. Whether chat apps exist is
// feature-detected: an App without them answers 404, and nothing shows.

const chatAppsConnectorPath = "/api/ha_nova/mcp"

var (
	errChatAppsUnsupported = errors.New("this NOVA App can't run chat apps")
	errChatAppsUnknown     = errors.New("the NOVA App's answer was lost; chat apps may be on")
	chatAppsDeviceID       = regexp.MustCompile(`^[0-9a-f]{32}$`)
	chatAppsService        = regexp.MustCompile(`^mobile_app_[a-z0-9_]{1,100}$`)
)

// Test seams.
var (
	chatAppsReadyWait = 5 * time.Minute
	chatAppsPollEvery = 2 * time.Second
)

// setupOwnerIngress is the owner's ingress session from this run's sign-in
// (memory only). Step 3 and the text path's question use it for chat apps.
var setupOwnerIngress *CloudIngressClient

// chatAppsState is the App's answer (nova/src/http/handlers/chat-apps.ts).
type chatAppsState struct {
	Available  bool    `json:"available"`
	Enabled    bool    `json:"enabled"`
	Unfinished bool    `json:"unfinished"`
	OwnerPhone *string `json:"owner_phone"`
	Restarting bool    `json:"restarting"`
}

// chatAppsRefused: the App answered with an error code (HA_TOO_OLD,
// HA_CONFIG_INVALID, CONFIG_UNSAFE, ...).
type chatAppsRefused struct {
	status int
	code   string
}

func (e *chatAppsRefused) Error() string {
	return fmt.Sprintf("the NOVA App refused chat apps (%d %s)", e.status, e.code)
}

type chatAppsPhone struct {
	Service  string `json:"service"`
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// chatAppsCall reads (body nil) or changes the App's chat-app state.
var chatAppsCall = func(ctx context.Context, client *CloudIngressClient, body map[string]any) (chatAppsState, error) {
	endpoint, raw := CloudEndpointChatApps, []byte(nil)
	if body != nil {
		endpoint = CloudEndpointChatAppsSet
		raw, _ = json.Marshal(body)
	}
	response, err := client.Do(ctx, endpoint, "", raw)
	if err != nil {
		return chatAppsState{}, err
	}
	defer response.Body.Close()
	data, err := readCloudResponse(response.Body, maxPairingBodyBytes, "read chat-app state")
	if err != nil {
		return chatAppsState{}, err
	}
	switch {
	case !response.ReachedRelay:
		return chatAppsState{}, newCloudHTTPError(CloudErrHAProtocol, "reach the NOVA App", response.StatusCode, true)
	case response.StatusCode == http.StatusNotFound:
		return chatAppsState{}, errChatAppsUnsupported // an App without chat apps (or before the release that turns them on)
	case response.StatusCode != http.StatusOK:
		return chatAppsState{}, &chatAppsRefused{status: response.StatusCode, code: relayPairingErrorCode(data)}
	}
	var envelope struct {
		OK   bool          `json:"ok"`
		Data chatAppsState `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || !envelope.OK {
		return chatAppsState{}, newCloudError(CloudErrHAProtocol, "decode chat-app state", err)
	}
	return envelope.Data, nil
}

// chatAppsHA is what this computer's Relay reads from Home Assistant.
type chatAppsHA struct {
	version string
	loaded  bool // the ha_nova integration's entry is loaded: the connector answers
}

// chatAppsReadHA reads Home Assistant's version and the integration's entry
// through this computer's Relay (read-only verbs).
var chatAppsReadHA = func(paths runtimePaths) (chatAppsHA, error) {
	s, err := newRelaySessionWithin(paths, verbOptions{}, 10*time.Second)
	if err != nil {
		return chatAppsHA{}, err
	}
	defer s.close()
	status, body, err := s.coreGET("/api/config")
	if err != nil {
		return chatAppsHA{}, err
	}
	var config struct {
		Version string `json:"version"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &config) != nil {
		return chatAppsHA{}, fmt.Errorf("Home Assistant /api/config answered %d", status)
	}
	var entries []struct {
		State string `json:"state"`
	}
	if err := s.wsDecode(map[string]any{"type": "config_entries/get", "domain": "ha_nova"}, &entries, "config entries"); err != nil {
		return chatAppsHA{}, err
	}
	ha := chatAppsHA{version: config.Version}
	for _, entry := range entries {
		ha.loaded = ha.loaded || entry.State == "loaded"
	}
	return ha, nil
}

// chatAppsHAVersionOK: Home Assistant 2026.10 or newer (the connector's
// sign-in needs it, spec §11.3).
func chatAppsHAVersionOK(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	year, errY := strconv.Atoi(parts[0])
	month, errM := strconv.Atoi(parts[1])
	return errY == nil && errM == nil && year*100+month >= 202610
}

// chatAppsOffer is what decides whether chat apps can be turned on here: the
// App has them, Home Assistant Link remote access answers, and Home Assistant
// is new enough. ok=false: say nothing.
type chatAppsOffer struct {
	state  chatAppsState
	url    string // the connector address
	loaded bool   // Home Assistant runs the integration: the address answers
}

func readChatAppsOffer(client *CloudIngressClient, paths runtimePaths, linkOrigin func() string) (chatAppsOffer, bool) {
	if client == nil {
		return chatAppsOffer{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := chatAppsCall(ctx, client, nil) // first: an App without chat apps answers 404 at once
	if err != nil || !state.Available {
		return chatAppsOffer{}, false
	}
	ha, err := chatAppsReadHA(paths)
	if err != nil || !chatAppsHAVersionOK(ha.version) {
		return chatAppsOffer{}, false
	}
	origin := strings.TrimRight(linkOrigin(), "/")
	if origin == "" {
		return chatAppsOffer{}, false
	}
	return chatAppsOffer{state: state, url: origin + chatAppsConnectorPath, loaded: ha.loaded}, true
}

// handOverOwnerPhone gives the App the owner's phone linked on this computer
// while chat apps are on and the App has none yet (spec §11.4: the owner taps
// once). The state's phone is updated when it took it; err when it didn't.
func handOverOwnerPhone(client *CloudIngressClient, paths runtimePaths, owner string, state *chatAppsState) error {
	phone := ownerChatPhone(paths, owner, *state)
	if phone == nil || !state.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fresh, err := chatAppsCall(ctx, client, nil) // the NOVA page may have set one meanwhile
	if err != nil {
		return err
	}
	if fresh.OwnerPhone != nil {
		state.OwnerPhone = fresh.OwnerPhone
		return nil
	}
	if _, err := chatAppsCall(ctx, client, map[string]any{"owner_phone": phone}); err != nil {
		return err
	}
	name := phone.Name
	state.OwnerPhone = &name
	return nil
}

// chatAppsPhoneName names the phone that will confirm for chat apps: the
// App's own, else the owner's link on this computer that goes along; ""
// means a code in the chat.
func chatAppsPhoneName(paths runtimePaths, owner string, state chatAppsState) string {
	if state.OwnerPhone != nil {
		return terminalSafeName(*state.OwnerPhone)
	}
	if phone := ownerChatPhone(paths, owner, state); phone != nil {
		return phone.Name
	}
	return ""
}

// ownerChatPhone is the owner's phone linked on this computer, offered as
// chat apps' phone too (spec §11.4: the owner taps once). nil when the link
// isn't the owner's own or the App has one already.
func ownerChatPhone(paths runtimePaths, owner string, state chatAppsState) *chatAppsPhone {
	links, ok := phoneLinks(paths)
	entry, found := links[owner]
	if !ok || !found || owner == "" || state.OwnerPhone != nil || entry.link == nil {
		return nil
	}
	l := entry.link
	name := terminalSafeName(l.Name)
	if l.UserID != owner || !chatAppsService.MatchString(l.Service) || !chatAppsDeviceID.MatchString(l.DeviceID) || name == "" || len(name) > 100 {
		return nil
	}
	return &chatAppsPhone{Service: l.Service, DeviceID: l.DeviceID, Name: name}
}

// turnOnChatApps asks the App to turn chat apps on. The caller showed "Home
// Assistant restarts once (about a minute)." and got the click: that is the
// restart consent (allow_restart). It then waits until Home Assistant runs
// the integration; progress hears "on", "restart" (when Home Assistant
// restarts), "back" (it answers again), and "ready". Then the owner's phone
// goes to the App in a request of its own (ingress runs through Home
// Assistant, so not while it restarts). ready=false: the App turned them on,
// but Home Assistant hasn't loaded them in time. ctx stops the wait.
func turnOnChatApps(ctx context.Context, client *CloudIngressClient, paths runtimePaths, before chatAppsState, phone *chatAppsPhone, progress func(string)) (chatAppsTurnOn, error) {
	call := func(body map[string]any, timeout time.Duration) (chatAppsState, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return chatAppsCall(callCtx, client, body)
	}
	state, err := call(map[string]any{"enabled": true, "allow_restart": true}, 2*time.Minute)
	var refused *chatAppsRefused
	// Answers that come before the App changes anything.
	clearRefusal := errors.Is(err, errChatAppsUnsupported) ||
		errors.As(err, &refused) && (refused.status < http.StatusInternalServerError || refused.code == "OWNER_CHECK_UNAVAILABLE")
	switch {
	case err == nil, clearRefusal, IsCloudErrorCode(err, CloudErrOuterSessionExpired):
	case ctx.Err() != nil || before.Enabled:
		// Stopped while asking, or on before: a re-read can't tell what this
		// request did.
		err = errChatAppsUnknown
	default:
		// The answer was lost (network, timeout, 5xx): the App may have
		// turned them on and Home Assistant may be restarting. Ask again.
		again, againErr := call(nil, 30*time.Second)
		switch {
		case againErr != nil:
			err = errChatAppsUnknown
		case again.Enabled:
			state, err = again, nil
		}
	}
	if err == nil && !state.Enabled {
		err = &chatAppsRefused{status: http.StatusOK, code: "NOT_ENABLED"}
	}
	if err != nil {
		return chatAppsTurnOn{}, err
	}
	result := chatAppsTurnOn{on: true}
	progress("on")
	if state.Restarting {
		progress("restart")
	}
	down, restarted := false, !state.Restarting // a restart to come: an entry loaded before it proves nothing
	for deadline := time.Now().Add(chatAppsReadyWait); !result.ready && time.Now().Before(deadline); {
		ha, err := chatAppsReadHA(paths)
		switch {
		case err != nil:
			down, restarted = true, true
		case ha.loaded && restarted:
			progress("ready")
			result.ready = true
			continue
		case down:
			down = false
			progress("back")
		}
		select {
		case <-ctx.Done(): // on, not loaded yet; the phone stays to hand over
			result.phoneFailed = phone != nil && state.OwnerPhone == nil
			return result, nil
		case <-time.After(chatAppsPollEvery):
		}
	}
	switch {
	case phone == nil || state.OwnerPhone != nil: // never over one the App got meanwhile
	case !result.ready:
		result.phoneFailed = true // Home Assistant isn't back: running chat-apps on again hands it over
	default:
		_, err := call(map[string]any{"owner_phone": phone}, 30*time.Second)
		result.phoneStored = err == nil
		result.phoneFailed = err != nil
	}
	return result, nil
}

type chatAppsTurnOn struct {
	on          bool // the App said on (also when stopped while waiting)
	ready       bool
	phoneStored bool // the owner's phone now confirms for chat apps
	phoneFailed bool // it should have, but the App didn't take it
}

// chatAppsProblem: one plain line for a turn-on that didn't work.
func chatAppsProblem(err error) setupMsg {
	var refused *chatAppsRefused
	switch {
	case IsCloudErrorCode(err, CloudErrOuterSessionExpired):
		return setupText("setup.chat.problem_expired")
	case errors.As(err, &refused) && refused.code == "HA_TOO_OLD":
		return setupText("setup.chat.problem_old")
	case errors.As(err, &refused) && refused.code == "HA_CONFIG_INVALID":
		return setupText("setup.chat.problem_config")
	case errors.As(err, &refused) && refused.code == "CONFIG_UNSAFE":
		return setupText("setup.chat.problem_folder")
	case errors.As(err, &refused) && (refused.code == "CONFIG_UNAVAILABLE" || refused.code == "CHAT_APPS_UNAVAILABLE"):
		return setupText("setup.chat.problem_app")
	case errors.Is(err, errChatAppsUnknown):
		return setupText("setup.chat.problem_unknown")
	}
	return setupText("setup.chat.problem_failed")
}

// printChatAppsHowTo: the connector address and the three steps per app.
func printChatAppsHowTo(out io.Writer, url string) {
	renderSetupParagraph(out, setupT("setup.chat.on"))
	fmt.Fprintf(out, "    %s\n\n", url)
	steps := func(title string, lines ...string) {
		fmt.Fprintf(out, "  %s\n", title)
		for i, line := range lines {
			fmt.Fprintf(out, "    %d. %s\n", i+1, line)
		}
		fmt.Fprintln(out)
	}
	steps(setupT("setup.chat.claude_title"), setupT("setup.chat.claude_1"), setupT("setup.chat.claude_2"), setupT("setup.chat.claude_3"))
	steps(setupT("setup.chat.chatgpt_title"), setupT("setup.chat.chatgpt_1"), setupT("setup.chat.chatgpt_2"), setupT("setup.chat.chatgpt_3"))
	renderSetupParagraph(out, setupT("setup.chat.off_hint"))
}

// askChatAppsConsent is the terminal's question (setup's text path and
// `ha-nova chat-apps on`): what it does and the restart on one screen, then
// an arrow menu. setupOffer is setup's question, whose default is "Not now".
func askChatAppsConsent(reader *bufio.Reader, out io.Writer, setupOffer bool, phone string) bool {
	consent := setupT("setup.chat.consent_code")
	if phone != "" {
		consent = setupT("setup.chat.consent_phone", "phone", phone)
	}
	renderSetupParagraph(out, setupT("setup.chat.key"), setupT("setup.chat.value"), consent, setupT("setup.chat.restart"))
	options := []setupMenuOption{{Value: "on", Label: setupT("setup.chat.turn_on")}, {Value: "skip", Label: setupT("setup.chat.not_now")}}
	choice := "on"
	if setupOffer {
		options[0], options[1] = options[1], options[0]
		choice = "skip"
	}
	armSetupNextPromptSkipsStaleBlankInput() // an Enter typed earlier never answers this
	defer clearSetupNextPromptSkipsStaleBlankInput()
	answer, err := promptSetupMenu(reader, out, setupMenuSpec{Prompt: "Use ↑/↓ or j/k, Enter to select", DefaultValue: choice, Options: options}, func() (string, error) {
		// Without the menu only a typed "yes" consents.
		answer, err := promptLineWithOptions(reader, out, "Type 'yes' to turn chat apps on (Home Assistant restarts once), or press Enter to skip", "", false)
		if err == nil && strings.EqualFold(strings.TrimSpace(answer), "yes") {
			return "on", nil
		}
		return "skip", nil
	})
	return err == nil && answer == "on"
}

// turnOnChatAppsInTerminal runs the turn-on after the consent, with its
// progress as lines. ok=false: it failed (one line said why).
func turnOnChatAppsInTerminal(out io.Writer, paths runtimePaths, client *CloudIngressClient, offer chatAppsOffer, owner string) (note string, ok bool) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := turnOnChatApps(ctx, client, paths, offer.state, ownerChatPhone(paths, owner, offer.state), func(event string) {
		switch event {
		case "on":
			renderSetupSuccessLine(out, "%s", setupT("setup.chat.row_on"))
		case "restart":
			fmt.Fprintf(out, "  %s\n", setupT("setup.chat.row_restart"))
		case "ready":
			renderSetupSuccessLine(out, "%s", setupT("setup.chat.row_ready"))
		}
	})
	switch {
	case err != nil:
		return chatAppsProblem(err).plain(), false
	case !result.ready:
		return setupT("setup.chat.problem_slow"), true
	case result.phoneFailed:
		return setupT("setup.chat.problem_phone"), true
	}
	return "", true
}
