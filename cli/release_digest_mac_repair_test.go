package main

import (
	"os"
	"strings"
	"testing"
	"text/template"
)

// The 0.31 CLI already on a Mac can't learn new update checks, so the
// release notes it reads (0.32's, and the latest stable's after it) carry the
// re-pair warning where its notice reads them: the headline (first new
// feature) and the action item (first What To Watch item). cli/release.go and cli/release_digest.go are unchanged since
// v0.31.0, so this is the 0.31 logic, run on the body GoReleaser publishes
// for a stable tag: .goreleaser.yml's release header and footer.
// scripts/release/prove-old-cli-digest.sh runs the real v0.31.0 binary on
// the same body. A direct `ha-nova update` on 0.31 prints no highlights, so
// only check-update and the agent's pre-update advice carry the warning.
func TestMacRepairWarningLeadsTheOldCLIUpdateNotice(t *testing.T) {
	const warning = "Updating a Mac from 0.31? HA NOVA asks you to pair it again once."
	highlights := deriveReleaseHighlights(publishedStableReleaseBody(t, "v0.36.0"))
	if got := releaseHeadline(highlights); got != warning {
		t.Fatalf("headline %q, want %q", got, warning)
	}
	if len(highlights) == 0 || highlights[0].Kind != releaseHighlightKindAction || !strings.HasPrefix(highlights[0].Text, warning) {
		t.Fatalf("first action item %+v, want it to start with %q", highlights, warning)
	}
	if text := updateReadyText("0.31.0", "0.36.0", "", highlights, ""); !strings.Contains(text, "New: "+warning) {
		t.Fatalf("notice %q lacks the warning", text)
	}
}

// publishedStableReleaseBody renders .goreleaser.yml's release header for a
// stable tag and appends its footer, as GoReleaser does with the changelog
// disabled.
func publishedStableReleaseBody(t *testing.T, tag string) string {
	t.Helper()
	raw, err := os.ReadFile("../.goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	header, err := template.New("header").Parse(goreleaserReleaseBlock(t, string(raw), "header"))
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	if err := header.Execute(&body, map[string]any{"Prerelease": false, "Tag": tag, "Version": strings.TrimPrefix(tag, "v")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), "## New Features") {
		t.Fatalf("the stable release header has no New Features section:\n%s", body.String())
	}
	return body.String() + "\n" + goreleaserReleaseBlock(t, string(raw), "footer")
}

// goreleaserReleaseBlock returns release.<key>'s literal block, dedented.
func goreleaserReleaseBlock(t *testing.T, yml, key string) string {
	t.Helper()
	_, rest, ok := strings.Cut(yml, "\nrelease:\n")
	if !ok {
		t.Fatal(".goreleaser.yml has no release section")
	}
	_, rest, ok = strings.Cut(rest, "\n  "+key+": |\n")
	if !ok {
		t.Fatalf(".goreleaser.yml has no release.%s literal block", key)
	}
	var lines []string
	for _, line := range strings.Split(rest, "\n") {
		if line != "" && !strings.HasPrefix(line, "    ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "    "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
