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
	if !strings.Contains(output.String(), "Opening in your browser:") {
		t.Fatalf("missing announcement line:\n%s", output.String())
	}
	// The URL gets its own indented line so long links wrap cleanly.
	if !strings.Contains(output.String(), "\n      http://ha.example:8123/profile/security\n") {
		t.Fatalf("missing URL line:\n%s", output.String())
	}
}

func TestOpenPrivateBrowserURLNeverPrintsIngressCapability(t *testing.T) {
	originalBrowser := openBrowserForSetup
	t.Cleanup(func() { openBrowserForSetup = originalBrowser })
	const privateTarget = "https://unit.ui.nabu.casa/api/hassio_ingress/private-capability/home"
	opened := ""
	openBrowserForSetup = func(target string) error {
		opened = target
		return nil
	}

	output := &strings.Builder{}
	openPrivateBrowserURL(output, privateTarget)

	if opened != privateTarget {
		t.Fatalf("opened = %q", opened)
	}
	if strings.Contains(output.String(), privateTarget) ||
		strings.Contains(output.String(), "private-capability") {
		t.Fatalf("private target entered command output: %q", output)
	}
	if !strings.Contains(output.String(), "Opening NOVA") {
		t.Fatalf("missing browser progress: %q", output)
	}
}
