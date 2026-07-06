package main

import (
	"strings"
	"testing"
)

func TestSetupDeeplinksUseInstanceLocalMyRedirect(t *testing.T) {
	haURL := "http://192.168.1.5:8123"

	appPage := haRelayAppPageURL(haURL)
	if appPage != "http://192.168.1.5:8123/_my_redirect/supervisor_addon?addon=2368fcfa_ha_nova_relay" {
		t.Fatalf("haRelayAppPageURL() = %q", appPage)
	}

	repo := haAddRepositoryURL(haURL)
	if repo != "http://192.168.1.5:8123/_my_redirect/supervisor_add_addon_repository?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova" {
		t.Fatalf("haAddRepositoryURL() = %q", repo)
	}

	if got := haProfileSecurityURL(haURL); got != "http://192.168.1.5:8123/profile/security" {
		t.Fatalf("haProfileSecurityURL() = %q", got)
	}
}

func TestOpenBrowserShowingURLPrintsTargetBeforeOpening(t *testing.T) {
	originalBrowser := openBrowserForSetup
	t.Cleanup(func() { openBrowserForSetup = originalBrowser })
	opened := ""
	openBrowserForSetup = func(url string) error {
		opened = url
		return nil
	}

	output := &strings.Builder{}
	openBrowserShowingURL(output, "http://ha.example:8123/profile/security")

	if opened != "http://ha.example:8123/profile/security" {
		t.Fatalf("opened = %q", opened)
	}
	if !strings.Contains(output.String(), "Opening in your browser: http://ha.example:8123/profile/security") {
		t.Fatalf("missing URL line:\n%s", output.String())
	}
}
