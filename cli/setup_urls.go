package main

import (
	"fmt"
	"io"
	"net/url"
)

const haNovaRelayAppSlug = "2368fcfa_ha_nova_relay"
const haNovaRepositoryURL = "https://github.com/markusleben/ha-nova"

// haRelayAppPageURL links to the NOVA Relay app page through the instance's
// own my-redirect endpoint so every Home Assistant version resolves its own
// panel path (older HA: /hassio/addon/<slug>/info, HA >= 2026.2 after the
// add-on -> app rename: /config/app/<slug>/info). Never hardcode either
// panel path here: the installed HA version is unknown at setup time.
func haRelayAppPageURL(haURL string) string {
	return haURL + "/_my_redirect/supervisor_addon?addon=" + haNovaRelayAppSlug
}

func haAddRepositoryURL(haURL string) string {
	return haURL + "/_my_redirect/supervisor_add_addon_repository?repository_url=" + url.QueryEscape(haNovaRepositoryURL)
}

func haProfileSecurityURL(haURL string) string {
	return haURL + "/profile/security"
}

func openBrowserShowingURL(out io.Writer, target string) {
	renderSetupLink(out, "Opening in your browser:", target)
	if err := openBrowserForSetup(target); err != nil {
		printHumanWarn("Could not open the browser automatically. Please open the link above yourself.")
	}
}

// openAnnouncedBrowserURL opens a target whose URL the wizard has just
// announced; it deliberately does not repeat the URL.
func openAnnouncedBrowserURL(out io.Writer, target string) {
	fmt.Fprintln(out, "  Opening in your browser...")
	if err := openBrowserForSetup(target); err != nil {
		printHumanWarn("Could not open the browser automatically. Please open the link shown above yourself.")
	}
}
