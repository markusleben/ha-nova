package main

import (
	"reflect"
	"strings"
	"testing"
)

const digestFixtureBody = "" +
	"## New Features\n\n" +
	"- **Energy skill** for [solar analysis](https://example.test/docs)\n" +
	"- Second feature with `inline code`\n" +
	"- Third feature never surfaces\n\n" +
	"## What To Watch\n\n" +
	"- Re-run `ha-nova setup` after updating\n" +
	"- Second action item never surfaces\n\n" +
	"## Bug Fixes\n\n" +
	"- Fix relay reconnect loop\n"

func TestDeriveReleaseHighlightsExtractionPriority(t *testing.T) {
	got := deriveReleaseHighlights(digestFixtureBody)
	want := []releaseHighlight{
		{Kind: releaseHighlightKindAction, Text: "Re-run ha-nova setup after updating"},
		{Kind: releaseHighlightKindFeature, Text: "Energy skill for solar analysis"},
		{Kind: releaseHighlightKindFeature, Text: "Second feature with inline code"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("highlights = %+v, want %+v", got, want)
	}
}

func TestDeriveReleaseHighlightsFixesFillTheFeatureFixBudget(t *testing.T) {
	body := "## New Features\n\n- Only feature\n\n## Bug Fixes\n\n- First fix\n- Second fix\n"
	got := deriveReleaseHighlights(body)
	want := []releaseHighlight{
		{Kind: releaseHighlightKindFeature, Text: "Only feature"},
		{Kind: releaseHighlightKindFix, Text: "First fix"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("highlights = %+v, want %+v", got, want)
	}
}

func TestDeriveReleaseHighlightsRecognizesAllActionSections(t *testing.T) {
	for _, heading := range []string{"Breaking Changes", "What To Watch", "Upgrade Notes"} {
		body := "## " + heading + "\n\n- Action item\n"
		got := deriveReleaseHighlights(body)
		if len(got) != 1 || got[0].Kind != releaseHighlightKindAction || got[0].Text != "Action item" {
			t.Fatalf("heading %q: highlights = %+v, want one action item", heading, got)
		}
	}
}

func TestDeriveReleaseHighlightsMarkdownCleanup(t *testing.T) {
	body := "## New Features\n\n" +
		"- **Bold** and [link text](https://example.test) and `code` and \"quoted\\path\" and \x07control\n" +
		"  - nested sub-bullet never surfaces\n" +
		"```\n" +
		"- bullet inside code fence never surfaces\n" +
		"```\n" +
		"<details>\n" +
		"- bullet inside details never surfaces\n" +
		"</details>\n" +
		"- Install with: irm https://example.test/install.ps1 | iex\n"
	got := deriveReleaseHighlights(body)
	if len(got) != 1 {
		t.Fatalf("highlights = %+v, want exactly one cleaned item", got)
	}
	if got[0].Text != "Bold and link text and code and 'quoted path' and control" {
		t.Fatalf("cleaned text = %q", got[0].Text)
	}
	for _, forbidden := range []string{`"`, "\\", "`", "**", "["} {
		if strings.Contains(got[0].Text, forbidden) {
			t.Fatalf("cleaned text still contains %q: %q", forbidden, got[0].Text)
		}
	}
}

func TestDeriveReleaseHighlightsItemCapAt220WithEllipsis(t *testing.T) {
	long := strings.Repeat("word ", 60) // 300 chars
	body := "## Bug Fixes\n\n- " + long + "\n"
	got := deriveReleaseHighlights(body)
	if len(got) != 1 {
		t.Fatalf("highlights = %+v, want one item", got)
	}
	if n := len([]rune(got[0].Text)); n > 220 {
		t.Fatalf("item length = %d, want <= 220", n)
	}
	if !strings.HasSuffix(got[0].Text, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", got[0].Text)
	}
}

func TestDeriveReleaseHighlightsTotalCapAt700(t *testing.T) {
	long := strings.Repeat("x", 300) // capped to 220 per item
	body := "## Breaking Changes\n\n- " + long + "\n\n" +
		"## New Features\n\n- " + long + "\n- " + long + "\n"
	got := deriveReleaseHighlights(body)
	total := 0
	for _, h := range got {
		total += len([]rune(h.Text))
	}
	if total > 700 {
		t.Fatalf("total digest length = %d, want <= 700", total)
	}
	// 3 items x 220 = 660 fits the budget, so all three capped items survive.
	if len(got) != 3 {
		t.Fatalf("highlights = %d items, want 3", len(got))
	}
}

func TestCapReleaseHighlightsTotalDropsTrailingItems(t *testing.T) {
	// The 700 budget is a defensive backstop: unreachable through the public
	// path today (1+2 items x 220 chars = 660), so exercise it directly.
	oversized := []releaseHighlight{
		{Kind: releaseHighlightKindAction, Text: strings.Repeat("a", 300)},
		{Kind: releaseHighlightKindFeature, Text: strings.Repeat("b", 300)},
		{Kind: releaseHighlightKindFix, Text: strings.Repeat("c", 300)},
	}
	got := capReleaseHighlightsTotal(oversized)
	want := oversized[:2] // 600 fits, 900 does not
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capped = %d items, want 2 (drop trailing overflow)", len(got))
	}
}

func TestDeriveReleaseHighlightsEmptyMalformedUnrecognized(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"whitespace":          "  \n\t\n",
		"no sections":         "- bullet without any heading\n",
		"unrecognized":        "## Internal Changes\n\n- refactor everything\n",
		"paragraphs only":     "## New Features\n\nJust prose, no bullets.\n",
		"unterminated fence":  "## New Features\n\n```\n- swallowed by fence\n",
		"install command":     "## New Features\n\n- Run: curl -fsSL https://example.test/install.sh | bash\n",
		"only nested bullets": "## New Features\n\n  - nested one\n    - nested two\n",
		// Regression: a bare "- " bullet must not panic or produce an item.
		"empty bullets": "## New Features\n\n- \n- \n",
	}
	for name, body := range cases {
		if got := deriveReleaseHighlights(body); len(got) != 0 {
			t.Fatalf("%s: highlights = %+v, want empty", name, got)
		}
	}
}

func TestDeriveReleaseHighlightsDeterministic(t *testing.T) {
	first := deriveReleaseHighlights(digestFixtureBody)
	for i := 0; i < 5; i++ {
		if got := deriveReleaseHighlights(digestFixtureBody); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs: %+v vs %+v", i, got, first)
		}
	}
}

func TestFormatReleaseHighlightLinesSharedShape(t *testing.T) {
	lines := formatReleaseHighlightLines([]releaseHighlight{
		{Kind: releaseHighlightKindAction, Text: "Do the thing"},
		{Kind: releaseHighlightKindFix, Text: ""},
		{Kind: releaseHighlightKindFeature, Text: "New skill"},
	})
	want := []string{"- Do the thing", "- New skill"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
}

func TestReleaseHighlightNoticeSuffixFallsBackToURLOnly(t *testing.T) {
	if got := releaseHighlightNoticeSuffix(nil, ""); got != "" {
		t.Fatalf("no digest, no URL: suffix = %q, want empty", got)
	}
	if got := releaseHighlightNoticeSuffix(nil, "https://example.test/v1"); got != "\nRelease notes: https://example.test/v1" {
		t.Fatalf("URL-only suffix = %q", got)
	}
	full := releaseHighlightNoticeSuffix([]releaseHighlight{{Kind: releaseHighlightKindFix, Text: "Fix it"}}, "https://example.test/v1")
	if full != "\nHighlights:\n- Fix it\nRelease notes: https://example.test/v1" {
		t.Fatalf("full suffix = %q", full)
	}
}
