package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"
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

// haLinkPageURL is Home Assistant's own Home Assistant Link page
// (frontend route /config/cloud), on the saved local address only;
// "" without one.
func haLinkPageURL(haURL string) string {
	if haURL == "" {
		return ""
	}
	return strings.TrimRight(haURL, "/") + "/config/cloud"
}

type haNOVAAppOpenTarget struct {
	URL    string
	Direct bool
}

func haNOVAAppPanelURL(
	haURL string,
) (haNOVAAppOpenTarget, error) {
	baseURL := strings.TrimRight(haURL, "/")
	identity := cloudRemoteBuildIdentityForRuntime()
	if identity.Disabled ||
		(!identity.Official && !identity.Development) {
		return haNOVAAppOpenTarget{URL: baseURL}, nil
	}
	if identity.Development &&
		validateCloudRemoteDevelopmentIdentity(identity) != nil {
		return haNOVAAppOpenTarget{URL: baseURL}, nil
	}
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		return haNOVAAppOpenTarget{}, err
	}
	return haNOVAAppOpenTarget{
		URL:    baseURL + "/app/" + url.PathEscape(appSlug),
		Direct: true,
	}, nil
}

// haInstallRelayAppURL opens the NOVA Relay App page and, when the repository
// is missing, asks once to add it. The supervisor_addon My redirect has taken
// the optional repository_url on every supported Home Assistant version
// (frontend hassio/src/hassio-my-redirect.ts + addon-view/hassio-addon-dashboard.ts
// before 2026.2; src/panels/my/ha-panel-my.ts + config/apps/ha-config-app-dashboard.ts
// since). It replaces the supervisor_add_addon_repository link
// (home-assistant/my.home-assistant.io#698) and the store search after it.
func haInstallRelayAppURL(haURL string) string {
	return haRelayAppPageURL(haURL) + "&repository_url=" + url.QueryEscape(haNovaRepositoryURL)
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

// openPrivateBrowserURL opens a process-local capability URL without writing
// it to stdout or stderr. In particular, Supervisor Ingress paths must not
// enter terminal transcripts or AI-visible command output.
func openPrivateBrowserURL(out io.Writer, target string) {
	fmt.Fprintln(out, "  Opening NOVA in your browser...")
	if err := openBrowserForSetup(target); err != nil {
		printHumanWarn(
			"Could not open NOVA automatically. Open Home Assistant in your browser and select NOVA from the sidebar.",
		)
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
