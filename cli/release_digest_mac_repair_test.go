package main

import (
	"os"
	"strings"
	"testing"
)

// The 0.31 CLI already on a Mac can't learn new update checks, so the 0.32
// release notes carry the re-pair warning where its notice reads them: the
// headline (first new feature) and the action item (first upgrade note).
// cli/release.go and cli/release_digest.go are unchanged since v0.31.0, so
// this is the 0.31 logic; scripts/release/prove-old-cli-digest.sh runs the
// real v0.31.0 binary on the same body. At release prep, point bodyPath at
// the archived body instead of deleting the test.
func TestMacRepairWarningLeadsTheOldCLIUpdateNotice(t *testing.T) {
	const bodyPath = "../docs/work/next-release-body.md"
	const warning = "On macOS, 0.32 asks you to pair this computer again once."
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	highlights := deriveReleaseHighlights(string(body))
	if got := releaseHeadline(highlights); got != warning {
		t.Fatalf("headline %q, want %q", got, warning)
	}
	if len(highlights) == 0 || highlights[0].Kind != releaseHighlightKindAction || !strings.HasPrefix(highlights[0].Text, warning) {
		t.Fatalf("first action item %+v, want it to start with %q", highlights, warning)
	}
	if text := updateReadyText("0.31.0", "0.32.0", "", highlights, ""); !strings.Contains(text, "New: "+warning) {
		t.Fatalf("notice %q lacks the warning", text)
	}
}
