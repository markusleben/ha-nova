package main

// Release-notes digest: derives a compact, deterministic set of highlights
// from a GitHub release body. Only the normalized highlights are cached in
// latest-release.json — never the full body. See issue #403.

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	releaseHighlightKindAction  = "action"
	releaseHighlightKindFeature = "feature"
	releaseHighlightKindFix     = "fix"

	// Caps from the approved spec: at most 1 action-needed item plus 2
	// feature/fix items, 220 chars per item, 700 chars total.
	releaseHighlightMaxFeatureFixItems = 2
	releaseHighlightItemMaxChars       = 220
	releaseHighlightTotalMaxChars      = 700
)

// releaseHighlight is one normalized release-notes bullet. Kind is one of
// action|feature|fix; Text is plain single-line prose (no Markdown, no
// double quotes — the session hook extracts it with grep).
type releaseHighlight struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

var (
	releaseImageRe      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	releaseLinkRe       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	releaseWhitespaceRe = regexp.MustCompile(`\s+`)
)

// releaseHighlightKindForSection maps a release-notes heading to a highlight
// kind. Only these recognized sections feed the compact update notice
// (docs/releasing.md → Release Notes Style); everything else is ignored.
func releaseHighlightKindForSection(heading string) string {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(strings.TrimSpace(heading), ":")))
	switch normalized {
	case "breaking changes", "what to watch", "upgrade notes":
		return releaseHighlightKindAction
	case "new features":
		return releaseHighlightKindFeature
	case "bug fixes":
		return releaseHighlightKindFix
	}
	return ""
}

// deriveReleaseHighlights extracts the compact highlight digest from a raw
// release body. Deterministic: the same body always yields the same digest.
// Empty, malformed, or unrecognized bodies yield nil (generic-notice
// fallback).
func deriveReleaseHighlights(body string) []releaseHighlight {
	if strings.TrimSpace(body) == "" {
		return nil
	}

	var action, features, fixes []string
	currentKind := ""
	inCodeFence := false
	inDetails := false
	for _, rawLine := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(rawLine)

		// Fenced code blocks never contribute highlight text.
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}

		// <details> blocks are collapsed presentation detail — skip them.
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "<details") {
			// A one-line <details>...</details> opens and closes here.
			if !strings.Contains(lower, "</details>") {
				inDetails = true
			}
			continue
		}
		if strings.Contains(lower, "</details>") {
			inDetails = false
			continue
		}
		if inDetails {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			currentKind = releaseHighlightKindForSection(strings.TrimLeft(trimmed, "#"))
			continue
		}
		if currentKind == "" {
			continue
		}
		// Top-level bullets only: an indented bullet is a nested sub-item.
		if !strings.HasPrefix(rawLine, "- ") && !strings.HasPrefix(rawLine, "* ") {
			continue
		}
		text := normalizeReleaseHighlightText(rawLine[2:])
		if text == "" || looksLikeInstallCommand(text) {
			continue
		}
		switch currentKind {
		case releaseHighlightKindAction:
			action = append(action, text)
		case releaseHighlightKindFeature:
			features = append(features, text)
		case releaseHighlightKindFix:
			fixes = append(fixes, text)
		}
	}

	// Bullet order expresses importance: keep the first bullet of each
	// category, features before fixes for the shared feature/fix budget.
	var selected []releaseHighlight
	if len(action) > 0 {
		selected = append(selected, releaseHighlight{Kind: releaseHighlightKindAction, Text: action[0]})
	}
	featureFixCount := 0
	for _, text := range features {
		if featureFixCount >= releaseHighlightMaxFeatureFixItems {
			break
		}
		selected = append(selected, releaseHighlight{Kind: releaseHighlightKindFeature, Text: text})
		featureFixCount++
	}
	for _, text := range fixes {
		if featureFixCount >= releaseHighlightMaxFeatureFixItems {
			break
		}
		selected = append(selected, releaseHighlight{Kind: releaseHighlightKindFix, Text: text})
		featureFixCount++
	}

	return capReleaseHighlightsTotal(selected)
}

// capReleaseHighlightsTotal drops trailing items once the digest would exceed
// the total budget. With the current 1+2 item and 220-char caps the budget is
// a defensive backstop (3 x 220 = 660), guarding future cap changes.
func capReleaseHighlightsTotal(selected []releaseHighlight) []releaseHighlight {
	total := 0
	var result []releaseHighlight
	for _, highlight := range selected {
		length := len([]rune(highlight.Text))
		if total+length > releaseHighlightTotalMaxChars {
			break
		}
		total += length
		result = append(result, highlight)
	}
	return result
}

// normalizeReleaseHighlightText turns one Markdown bullet into plain
// single-line prose: control characters removed, presentation-only Markdown
// (bold, links, inline code) reduced to its text, double quotes and
// backslashes replaced so the session hook's grep-based JSON extraction stays
// exact, whitespace collapsed, and the 220-char item cap applied.
func normalizeReleaseHighlightText(raw string) string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	text = releaseImageRe.ReplaceAllString(text, "$1")
	text = releaseLinkRe.ReplaceAllString(text, "$1")
	replacer := strings.NewReplacer("**", "", "__", "", "`", "", `"`, "'", `\`, " ")
	text = replacer.Replace(text)
	text = strings.TrimSpace(releaseWhitespaceRe.ReplaceAllString(text, " "))

	runes := []rune(text)
	if len(runes) > releaseHighlightItemMaxChars {
		text = strings.TrimSpace(string(runes[:releaseHighlightItemMaxChars-3])) + "..."
	}
	return text
}

// looksLikeInstallCommand filters bullets that are really shell install
// instructions — those live in the release page and the pinned update
// guidance, not in the compact digest.
func looksLikeInstallCommand(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"| iex", "irm https://", "install.ps1", "install.sh", "curl -fssl"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// formatReleaseHighlightLines is the ONE shared formatter for the compact
// highlight lines. Every Go presentation path renders these exact lines; the
// Claude session hook reproduces the same "- <text>" shape from the cached
// "text" fields.
func formatReleaseHighlightLines(highlights []releaseHighlight) []string {
	lines := make([]string, 0, len(highlights))
	for _, highlight := range highlights {
		text := strings.TrimSpace(highlight.Text)
		if text == "" {
			continue
		}
		lines = append(lines, "- "+text)
	}
	return lines
}

// releaseHighlightNoticeSuffix composes the digest block that is appended
// AROUND (after) the pinned update-guidance message: highlight lines plus the
// release URL. With no valid digest it degrades to just the release URL, and
// to an empty string when that is missing too — the generic notice stays
// untouched either way.
func releaseHighlightNoticeSuffix(highlights []releaseHighlight, htmlURL string) string {
	var b strings.Builder
	if lines := formatReleaseHighlightLines(highlights); len(lines) > 0 {
		b.WriteString("\nHighlights:\n")
		b.WriteString(strings.Join(lines, "\n"))
	}
	if url := strings.TrimSpace(htmlURL); url != "" {
		b.WriteString("\nRelease notes: ")
		b.WriteString(url)
	}
	return b.String()
}
