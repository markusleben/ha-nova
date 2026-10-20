package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// `ha-nova chat-apps [on|off|status]` (spec §11.6): the owner's chat-app
// switch from the terminal, for the computer that is already set up. on and
// off sign the owner in to Home Assistant in the browser and call the NOVA
// App's owner-only action, as setup's step 3 does; on asks the restart
// consent on the same screen first. status reads Home Assistant through
// this computer's Relay, without a sign-in. Not listed in the usage until
// the App release that turns chat apps on.

// Test seams.
var (
	chatAppsSignIn = func(out io.Writer, haURL string) (haSignInSession, error) {
		// No code to fall back to, but NOVA in Home Assistant works on any device.
		return openHASignInIngressNoCode(context.Background(), out, haURL, chatAppsElsewhereLine)
	}
	chatAppsSignInOK           = haSignInPossible
	chatAppsStdin    io.Reader = os.Stdin
)

const chatAppsAppTooOldLine = "Your NOVA App can't run chat apps yet. Update it in Home Assistant, then run this again."

const chatAppsElsewhereLine = "Can't sign in on this computer? Press Ctrl+C. Then do it in Home Assistant on any device: open NOVA in the sidebar, then Chat apps."

const chatAppsUsage = "ha-nova chat-apps [on|off|status] [--server <name>]\n\n" +
	"Turns HA NOVA in Claude and ChatGPT on or off (Home Assistant owner only), or shows whether it is on."

func runChatAppsCommand(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("chat-apps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	server := fs.String("server", "", "server profile name (multi-server installs)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, chatAppsUsage) {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	action := "status"
	if rest := fs.Args(); len(rest) > 0 { // flags may also follow the action
		action = rest[0]
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() > 0 {
			if helpRequested(err, fs, chatAppsUsage) {
				return 0
			}
			action = ""
		}
	}
	if action != "on" && action != "off" && action != "status" {
		printHumanErr("Usage: ha-nova chat-apps [on|off|status] [--server <name>]")
		return 1
	}
	if *server != "" {
		setServerSelectionOverride(*server)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if cfg.RelayBaseURL == "" || (cfg.HAURL == "" && action != "status") { // status needs only the Relay
		printHumanErr("HA NOVA isn't connected to Home Assistant on this computer yet. Run ha-nova setup first.")
		return 1
	}
	out := os.Stdout
	if action == "off" { // also when Home Assistant can't be read
		return chatAppsOff(out, paths, cfg)
	}
	ha, err := chatAppsReadHA(paths)
	if err != nil {
		printHumanErr("HA NOVA couldn't read Home Assistant: %s", err)
		return 1
	}
	origin := setupLinkRemoteOrigin(cfg)
	if origin == "" && cfg.Cloud.ready() && !chatAppsAtHome(cfg) { // away from home the local check can't run: the saved Link address
		origin = cfg.Cloud.Current.Origin
	}
	url := ""
	if base := chatAppsBase(origin, ha.externalURL); base != "" {
		url = base + chatAppsConnectorPath
	}
	switch action {
	case "status":
		switch {
		case ha.leftover():
			fmt.Fprintf(out, "  %s\n", chatAppsLeftoverLine)
		case ha.appTooOld():
			fmt.Fprintln(out, "  Your NOVA App can't run chat apps yet. To use them, update it in Home Assistant.")
		case ha.present && !ha.loaded: // Home Assistant holds them, but doesn't run them (yet)
			fmt.Fprintln(out, "  Home Assistant hasn't started chat apps yet. Wait a minute, then check again. If it stays like this, open NOVA in Home Assistant, then Chat apps, and do what it says there.")
		case !ha.loaded: // off, or on with Home Assistant's restart still to come: either way they don't run, and `on` finishes both
			fmt.Fprintln(out, "  Chat apps aren't running in Home Assistant. To turn them on: ha-nova chat-apps on")
		case url != "":
			printChatAppsHowTo(out, url)
		default:
			renderSetupParagraph(out, setupT("setup.chat.on_noaddress"), setupT("setup.chat.off_hint"))
		}
		return 0
	}
	switch {
	case !chatAppsHAVersionOK(ha.version):
		printHumanErr("%s", setupT("setup.chat.problem_old"))
		return 1
	case ha.appTooOld():
		printHumanErr("%s", chatAppsAppTooOldLine)
		return 1
	case url == "":
		printHumanErr("%s", setupT("setup.chat.need_address_cli"))
		return 1
	case !chatAppsSignInOK(out, cfg):
		printHumanErr("Turning chat apps on needs a sign-in in a browser on this computer. Use NOVA in Home Assistant instead: open NOVA in the sidebar, then Chat apps.")
		return 1
	}
	session, ok := chatAppsOwnerSession(out, cfg)
	if !ok {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	state, err := chatAppsCall(ctx, session.ingress, nil)
	cancel()
	if err == nil && !state.Available {
		err = errChatAppsUnsupported
	}
	if errors.Is(err, errChatAppsUnsupported) {
		printHumanErr("%s", chatAppsAppTooOldLine)
		return 1
	}
	if err != nil {
		renderSetupWarningLine(out, "%s", chatAppsProblem(err).plain())
		return 1
	}
	if state.Enabled && ha.loaded { // on already; the owner's phone may still be missing there
		if err := handOverOwnerPhone(session.ingress, paths, session.user.ID, &state); err != nil {
			renderSetupWarningLine(out, "%s", setupT("setup.chat.problem_phone"))
		}
		printChatAppsHowTo(out, url)
		return 0
	}
	// The consent after the sign-in: it names the phone that will confirm.
	if !askChatAppsConsent(bufio.NewReader(chatAppsStdin), out, false, !ha.running, chatAppsPhoneName(paths, session.user.ID, state)) {
		renderSetupParagraph(out, setupT("setup.chat.skipped"))
		return 0
	}
	note, on := turnOnChatAppsInTerminal(out, paths, session.ingress, chatAppsOffer{state: state, url: url, restart: !ha.running}, session.user.ID)
	if note != "" {
		renderSetupWarningLine(out, "%s", note)
	}
	if !on {
		return 1
	}
	printChatAppsHowTo(out, url)
	return 0
}

// chatAppsAtHome: this computer reaches the Relay locally, so the local Link
// check above is the answer (remote access is off), not a guess.
var chatAppsAtHome = func(cfg runtimeConfig) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport, err := resolveRelayTransport(ctx, resolveLocalRelayTransportForCLI, cfg)
	return err == nil && transport.Via == relayViaLocal
}

// chatAppsOwnerSession signs the owner in (browser) for the App's owner
// action.
func chatAppsOwnerSession(out io.Writer, cfg runtimeConfig) (haSignInSession, bool) {
	origin, err := resolveHASignInOriginForSetup(context.Background(), cfg.HAURL) // 8123 → 80 on Home Assistant OS
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, HACurrentUser{}))
		return haSignInSession{}, false
	}
	session, err := chatAppsSignIn(out, origin.String())
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, session.user))
		return session, false
	}
	return session, true
}

func chatAppsOff(out io.Writer, paths runtimePaths, cfg runtimeConfig) int {
	// An App without chat apps: say so before any sign-in (status knows it
	// too). Home Assistant unreadable: the sign-in path below decides.
	if ha, err := chatAppsReadHA(paths); err == nil && ha.appTooOld() {
		if ha.leftover() {
			printHumanErr("%s", chatAppsLeftoverLine)
			return 1
		}
		fmt.Fprintln(out, "  This NOVA App has no chat apps, so there is nothing to turn off.")
		return 0
	}
	if !chatAppsSignInOK(out, cfg) {
		printHumanErr("Turning chat apps off needs a sign-in in a browser on this computer. Use NOVA in Home Assistant instead: open NOVA in the sidebar, then Chat apps, then Turn off chat apps.")
		return 1
	}
	session, ok := chatAppsOwnerSession(out, cfg)
	if !ok {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := chatAppsCall(ctx, session.ingress, map[string]any{"enabled": false})
	// What Home Assistant holds decides, not only the App's answer: after
	// going back to an older App, Home Assistant may still have chat apps.
	ha, readErr := chatAppsReadHA(paths)
	held := readErr == nil && (ha.loaded || ha.present)
	switch {
	case errors.Is(err, errChatAppsUnsupported) && readErr != nil:
		printHumanErr("This NOVA App has no chat apps. HA NOVA couldn't reach Home Assistant to check for leftovers. Make sure Home Assistant is running, then run this again.")
		return 1
	case errors.Is(err, errChatAppsUnsupported) && held:
		printHumanErr("%s", chatAppsLeftoverLine)
		return 1
	case errors.Is(err, errChatAppsUnsupported):
		fmt.Fprintln(out, "  This NOVA App has no chat apps, so there is nothing to turn off.")
		return 0
	case err != nil:
		printHumanErr("Turning chat apps off didn't finish (%s). Run this again, or use NOVA in Home Assistant: Chat apps, then Turn off chat apps.", err)
		return 1
	case held:
		printHumanErr("Home Assistant still has HA NOVA for chat apps. Run ha-nova chat-apps off again.")
		return 1
	}
	renderSetupSuccessLine(out, "Chat apps are off. Claude and ChatGPT can't reach your home through HA NOVA anymore.")
	fmt.Fprintln(out, "  To sign them out of Home Assistant too, remove their tokens in Home Assistant under your profile → Security.")
	return 0
}
