package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// This computer's window approval on the NOVA App (0.32 spec §1, §3): the
// computer itself can only turn it off; turning it on is the owner's, on
// the NOVA page.

// gateWindowOff turns this computer's window approval off on the App. An
// App without window approval, or a connection that isn't a paired
// computer, answers 404: there is nothing to turn off.
var gateWindowOff = func(paths runtimePaths) error {
	s, err := newRelaySessionWithin(paths, verbOptions{}, 10*time.Second)
	if err != nil {
		return err
	}
	return s.gateWindowOff()
}

// gateWindowOffForProfile: the same for one server profile's paired
// computer, leaving the process's profile selection alone. pending: a
// pairing's key not yet promoted here, which the App may already hold as
// active; it goes only to the pending endpoint and pin saved with it (never
// the live one, which may be another Relay), and without them, or when the
// App doesn't take it (not activated yet), nothing is sent or copied.
func gateWindowOffForProfile(paths runtimePaths, profile, pending string) error {
	cfg, err := loadSelectedConfig(paths, profile, "--server", false)
	if err != nil {
		return err
	}
	unfinished := fmt.Errorf("its pairing didn't finish; finish it first with: ha-nova pair --server %s, then run this again", profile)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	transport := relayTransportSelection{BaseURL: cfg.PendingSecureBaseURL, Client: spkiPinnedClient(cfg.PendingSpkiPin), Credential: pending, DeviceMode: true, Via: relayViaLocal}
	switch {
	case pending != "" && (cfg.PendingSecureBaseURL == "" || cfg.PendingSpkiPin == ""):
		cancel()
		return unfinished
	case pending == "":
		if transport, err = selectRelayTransport(ctx, cfg, "", false); err != nil {
			cancel()
			return errors.New(relayTransportErrorMessage(err))
		}
	}
	err = relaySessionOn(paths, cfg, transport, ctx, cancel).gateWindowOff()
	var call *relayCallError
	if pending != "" && errors.As(err, &call) && (call.status == http.StatusUnauthorized || call.status == http.StatusForbidden) {
		return unfinished
	}
	return err
}

func (s *relaySession) gateWindowOff() error {
	defer s.close()
	var answer struct {
		Window bool `json:"window"`
	}
	err := s.gateCall(context.Background(), gateAskRoute, 10*time.Second, map[string]any{"op": "window_off"}, &answer)
	var call *relayCallError
	if errors.As(err, &call) && call.status == http.StatusNotFound {
		return nil
	}
	return err
}

const confirmWindowUsage = "ha-nova setup confirm-window on|off"

// runConfirmWindowSetting turns confirmation windows on or off. Off moves
// every typed plan to a code in chat, which the model sees too, so the
// window itself confirms it: a model that runs this command can't click it.
// On needs no confirmation.
func runConfirmWindowSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmWindowUsage + "\n\nWhere a typed change (unlock, open, disarm, delete) asks you: \"on\" (default) opens a\nconfirmation window on this computer when one can open; \"off\" uses your linked phone, else your app's\nconfirmation form, or a four-digit code in chat that your AI assistant can see too. Turning it off is confirmed in that window.")
		return 0
	}
	if len(args) != 1 || args[0] != "on" && args[0] != "off" {
		printHumanErr("Usage: %s", confirmWindowUsage)
		return 1
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot read the HA NOVA configuration (run ha-nova setup first): %s", err)
		return 1
	}
	if args[0] == "off" {
		// This computer's window approval on the NOVA App goes first: turning
		// it off only takes authority away, so it needs no confirmation.
		if err := gateWindowOff(paths); err != nil {
			printHumanErr("Nothing changed: the NOVA App didn't confirm that this computer stopped confirming in its window (%s). Try again, or turn it off on the NOVA page.", err)
			return 1
		}
	}
	if args[0] == "off" && !windowOff(paths) {
		if !windowAvailable() {
			printHumanInfo("This computer no longer confirms in its window on the NOVA App. No confirmation window can open here, so HA NOVA already asks on your linked phone, in your app's form, or with a four-digit code in chat.")
			return 0
		}
		lang := "en" // Home Assistant's language, when it can be read soon
		if s, err := newRelaySessionWithin(paths, verbOptions{}, 5*time.Second); err == nil {
			lang = s.homeLanguage()
			s.close()
		}
		switch askWindow(context.Background(), plainWindow(lang, confirmT(lang, "settings.window_off")), cliWindowWait, os.Stderr) {
		case windowConfirmed:
		case windowFailed:
			printHumanInfo("This computer no longer confirms in its window on the NOVA App. The confirmation window couldn't open here, so HA NOVA asks on your linked phone, in your app's form, or with a four-digit code in chat anyway.")
			return 0
		default:
			printHumanErr("This computer no longer confirms in its window on the NOVA App, but the window still opens here: it was cancelled or got no answer. If no window appeared at all, set \"%s\": \"off\" in %s yourself.", windowSetting, paths.ConfigFile)
			return 1
		}
	}
	top := make(map[string]json.RawMessage, len(doc.top))
	for key, value := range doc.top {
		top[key] = value
	}
	delete(top, windowSetting)
	if args[0] == "off" {
		top[windowSetting] = json.RawMessage(`"off"`)
	}
	if err := writeJSONFileIfUnchanged(paths.ConfigFile, top, 0o600, doc.source); err != nil {
		printHumanErr("cannot save the setting: %s", err)
		return 1
	}
	printHumanInfo("Confirmation windows are %s.", args[0])
	if args[0] == "on" {
		printHumanInfo("To let this computer confirm risky changes in its window, the Home Assistant owner turns it on for this computer on the NOVA page.")
	}
	return 0
}
