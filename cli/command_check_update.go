package main

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// quietUpdateCheckTimeout bounds the quiet first-use check's GitHub lookup.
var quietUpdateCheckTimeout = 3 * time.Second

// appUpdateTooLine: the HA NOVA notice names a due NOVA App update too, so
// one offer covers both (the App keeps its own preview and confirmation).
const appUpdateTooLine = "The NOVA App in Home Assistant needs an update too."

// withAppUpdateToo puts appUpdateTooLine into the HA NOVA notice, above its
// update step, when Home Assistant reports a pending NOVA App update too (a
// standalone Relay or an App with nothing pending gets no such promise).
func withAppUpdateToo(notice, relayNotice humanNotice) humanNotice {
	if notice.kind == humanNoticeKindUpdateAvailable && relayNotice.appUpdate {
		notice.message = strings.Replace(notice.message, "\nTo update", "\n"+appUpdateTooLine+"\nTo update", 1)
	}
	return notice
}

func runCheckUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("check-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	quiet := fs.Bool("quiet", false, "print only when an update is available; stay silent when current")
	jsonOutput := fs.Bool("json", false, "machine-readable result on stdout")
	server := fs.String("server", "", "server profile whose Relay is checked (multi-server installs)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova check-update [--quiet] [--json] [--server <name>]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	if err := selectServerFlag(fs, *server); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	client := httpClient
	if *quiet && !*jsonOutput {
		// The quiet check runs before a user's first task: a slow network
		// must never hold that task up. On a timeout the cached release
		// still answers; with no cache, quiet stays silent.
		client = &http.Client{Transport: httpClient.Transport, CheckRedirect: httpClient.CheckRedirect, Timeout: quietUpdateCheckTimeout}
	}
	result := buildUpdateCheckResultWithClient(paths, client)
	if *jsonOutput { // the CLI/skills result only: no server involved
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			printHumanErr("%s", err)
			return 1
		}
		return updateCheckExitCode(result)
	}
	if *server != "" { // a mistyped server must not count as checked
		if _, err := loadConfig(paths); err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}

	// Human/quiet path only (the `--json` branch above stays machine-clean): every
	// client runs `check-update` on first skill use per session, so this is the
	// universal point to self-heal client integrations once after a version change.
	// Best-effort; never blocks the update check.
	migrationContended := false
	if *quiet {
		_, migrationContended = repairMissingSessionBootstrapWithContention(paths)
	}
	ensureClientsVerifiedForCurrentVersion(paths)
	defer markSessionBootstrapLayoutVerified(paths)

	notice := humanNoticeFromUpdateCheckResult(result, *quiet)
	// CLI/skills freshness is only half the answer: the relay in Home
	// Assistant has its own version, and "up to date" would be misleading
	// below min_relay_version or while an App update is pending. The quiet
	// first-skill-use path must include this exact check too: relay response
	// headers prove only the compatibility floor and cannot expose a compatible
	// above-floor App update. Stderr-only, exit code unchanged; --json returned
	// above and stays machine-clean.
	var relayNotice humanNotice
	if *quiet {
		relayNotice = relayUpdateNoticeWithTimeout(paths, firstUseRelayNoticeTimeout)
	} else {
		relayNotice = relayUpdateNotice(paths)
	}
	notice = withAppUpdateToo(notice, relayNotice)
	printHumanNotice(notice)
	printHumanNotice(relayNotice)
	if *quiet {
		deadline := time.Now()
		if migrationContended {
			deadline = deadline.Add(sessionBootstrapCarrierContentionWait)
		}
		finalizePendingSessionBootstrapCarrierUntil(paths, deadline)
	}
	if notice.empty() {
		return 0
	}
	return updateCheckExitCode(result)
}
