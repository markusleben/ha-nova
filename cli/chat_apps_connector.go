package main

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The chat-app connector's address, Claude's Add to Claude link, and how to
// add it (docs/work/2026-10-19-chat-bridge-contract.md, "Setup and CLI (B4)").

// chatAppsBase is the address Claude and ChatGPT reach this home at: Home
// Assistant Link's while its remote access is on, else Home Assistant's own
// external URL when it is public HTTPS; "" when neither. Never http://:
// claude.ai and ChatGPT need HTTPS with a valid certificate.
func chatAppsBase(linkOrigin, externalURL string) string {
	if origin := strings.TrimRight(linkOrigin, "/"); origin != "" {
		return origin
	}
	if origin := publicHTTPSOrigin(externalURL); !strings.HasSuffix(origin, ".nabu.casa") && !strings.Contains(origin, ".nabu.casa:") {
		return origin // a Link address there counts only with remote access on (above)
	}
	return ""
}

// publicHTTPSOrigin: raw's https origin when it names a host on the
// internet, else "". ponytail: IP literals (any form: the last label must
// start with a letter) and local-only names (no dot, .local, .lan, .ts.net,
// ...) are refused outright; a public IP with a valid certificate is rare
// enough to leave out. Same rules as nova/src/runtime/chat-apps.ts
// (shared vectors: tests/fixtures/chat-connector-base.json).
func publicHTTPSOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.HasSuffix(u.Host, ":") {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if net.ParseIP(host) != nil || !publicHostname.MatchString(host) {
		return ""
	}
	for _, local := range []string{".local", ".lan", ".home", ".internal", ".localdomain", ".localhost", ".home.arpa", ".ts.net"} {
		if strings.HasSuffix(host, local) {
			return ""
		}
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || port[0] == '0' {
			return ""
		}
		return "https://" + net.JoinHostPort(host, port)
	}
	return "https://" + host
}

var publicHostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// claudeConnectorLink opens Claude's "Add custom connector" with the name
// and this home's connector address filled in (Claude's documented install
// link: https://claude.com/docs/connectors/building/directory-vs-custom).
// It always goes to claude.ai and carries the address only, never a token;
// Claude shows the values and asks before it adds anything.
func claudeConnectorLink(connectorURL string) string {
	return claudeLink("customize", connectorURL)
}

// claudeOrgConnectorLink: the same for a Team or Enterprise owner, whose
// members can't add a custom connector themselves (organization settings).
func claudeOrgConnectorLink(connectorURL string) string {
	return claudeLink("admin-settings", connectorURL)
}

func claudeLink(section, connectorURL string) string {
	encode := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "https://claude.ai/" + section + "/connectors?modal=add-custom-connector&connectorName=" + encode("HA NOVA") + "&connectorUrl=" + encode(connectorURL)
}

// printChatAppsHowTo: the connector address, the Add to Claude link, and
// the three steps per app.
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
	fmt.Fprintf(out, "  %s\n    %s\n\n", setupT("setup.chat.add_claude_cli"), claudeConnectorLink(url))
	fmt.Fprintf(out, "  %s\n    %s\n", setupT("setup.chat.org"), claudeOrgConnectorLink(url))
	fmt.Fprintf(out, "  %s\n\n", setupT("setup.chat.no_form_above"))
	steps(setupT("setup.chat.claude_title"), setupT("setup.chat.claude_1"), setupT("setup.chat.claude_2"), setupT("setup.chat.claude_3"))
	steps(setupT("setup.chat.chatgpt_title"), setupT("setup.chat.chatgpt_1"), setupT("setup.chat.chatgpt_2"), setupT("setup.chat.chatgpt_3"))
	renderSetupParagraph(out, setupT("setup.chat.off_hint"))
}
