package main

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func diffLines(t *testing.T, beforeJSON, afterJSON string) []string {
	t.Helper()
	bv, err := decodeJSONNumber([]byte(beforeJSON))
	if err != nil {
		t.Fatalf("before parse: %v", err)
	}
	av, err := decodeJSONNumber([]byte(afterJSON))
	if err != nil {
		t.Fatalf("after parse: %v", err)
	}
	bm, _ := bv.(map[string]interface{})
	am, _ := av.(map[string]interface{})
	return renderConfigChanges(bm, am)
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDiffScalarChange(t *testing.T) {
	got := diffLines(t, `{"mode":"single"}`, `{"mode":"restart"}`)
	assertLines(t, got, []string{"| Mode | single | restart |"})
}

func TestDiffMissingVsEmptyIsNotAChange(t *testing.T) {
	// Regression for the real-HA drift false positive: HA stores empty optional
	// fields (description: "", conditions: []) that a filtered draft/read-back
	// omits. Missing-vs-empty must render NOTHING and must not read as drift.
	assertLines(t, diffLines(t, `{"alias":"X"}`, `{"alias":"X","description":""}`), nil)
	assertLines(t, diffLines(t, `{"alias":"X","conditions":[]}`, `{"alias":"X"}`), nil)
	assertLines(t, diffLines(t, `{"alias":"X","data":{}}`, `{"alias":"X"}`), nil)
	assertLines(t, diffLines(t, `{"alias":"X","note":null}`, `{"alias":"X"}`), nil)
}

func TestDiffEscapesMultilineStringValues(t *testing.T) {
	// A multiline string value (automation description/template) must not embed a
	// raw newline/tab: it would structurally break the GFM table the skill prints
	// verbatim, not just one line.
	got := diffLines(t, `{"description":"old\nline"}`, `{"description":"new\tval"}`)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if strings.ContainsAny(got[0], "\n\r\t") {
		t.Fatalf("diff line must not contain a raw control char: %q", got[0])
	}
	if n := unescapedPipes(got[0]); n != 4 {
		t.Fatalf("row must keep exactly 4 unescaped pipes, got %d: %q", n, got[0])
	}
}

// unescapedPipes counts the structural pipes of a table row (escaped `\|`
// sequences inside cell content do not count).
func unescapedPipes(line string) int {
	return strings.Count(line, "|") - strings.Count(line, `\|`)
}

func TestDiffEscapesPipesInCells(t *testing.T) {
	// Jinja templates love pipes; an unescaped `|` inside a cell would add a
	// phantom column and shift Before/After.
	got := diffLines(t,
		`{"value_template":"{{ states('sensor.x') }}"}`,
		`{"value_template":"{{ states('sensor.x') | float }}"}`)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if !strings.Contains(got[0], `\| float`) {
		t.Fatalf("pipe in cell content must be escaped: %q", got[0])
	}
	if n := unescapedPipes(got[0]); n != 4 {
		t.Fatalf("row must keep exactly 4 unescaped pipes, got %d: %q", n, got[0])
	}
}

func TestDiffCellTruncationIsRuneSafe(t *testing.T) {
	// 79 ASCII bytes + a two-byte umlaut straddling the 80-byte boundary: the cut
	// must land on the rune start, keep valid UTF-8, and mark the cut with `…`.
	long := strings.Repeat("a", 79) + "ä-und-noch-mehr-text"
	got := diffLines(t, `{"description":"short"}`, `{"description":"`+long+`"}`)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if !utf8.ValidString(got[0]) {
		t.Fatalf("truncated row must stay valid UTF-8: %q", got[0])
	}
	if !strings.Contains(got[0], strings.Repeat("a", 79)+"…") {
		t.Fatalf("expected rune-safe cut before the umlaut plus …, got %q", got[0])
	}
	// A value of exactly 80 bytes passes through whole.
	exact := strings.Repeat("b", 80)
	got = diffLines(t, `{"description":"short"}`, `{"description":"`+exact+`"}`)
	if !strings.Contains(got[0], exact+" |") || strings.Contains(got[0], "…") {
		t.Fatalf("80-byte value must not be truncated: %q", got[0])
	}
}

func TestDiffLiteralEmDashValueIsQuoted(t *testing.T) {
	// A user value that IS the em dash must not render identical to the
	// absence marker — `| Note | — | — |` would show no visible difference.
	assertLines(t, diffLines(t, `{"alias":"X"}`, `{"alias":"X","note":"—"}`),
		[]string{`| Note | — | "—" |`})
	assertLines(t, diffLines(t, `{"note":"—"}`, `{"note":"real text"}`),
		[]string{`| Note | "—" | real text |`})
}

func TestDiffTypeSuffixSurvivesLongValues(t *testing.T) {
	// A string that contains compact JSON vs the equivalent object renders
	// identically; when that shared text is longer than a cell, the type
	// suffix must survive the cap — it is the only visible change.
	inner := `{"a":"` + strings.Repeat("x", 90) + `"}`
	before := `{"payload":` + `"` + strings.ReplaceAll(inner, `"`, `\"`) + `"` + `}`
	after := `{"payload":` + inner + `}`
	got := diffLines(t, before, after)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if !strings.Contains(got[0], "… (string) |") || !strings.Contains(got[0], "… (object) |") {
		t.Fatalf("type suffixes must survive truncation: %q", got[0])
	}
}

func TestDiffLateDivergenceStaysVisible(t *testing.T) {
	// Two long notification texts sharing their first >80 bytes must NOT render
	// as two identical `<prefix>…` cells — the first difference has to be
	// visible, or the user confirms copy they never saw (write-safety:
	// changed notification text must show old and new).
	shared := strings.Repeat("Der Plan wurde erstellt und geprüft. ", 3) // 111 bytes shared prefix
	before := `{"actions":[{"action":"notify.phone","data":{"message":"` + shared + `Start um 06:30."}}]}`
	after := `{"actions":[{"action":"notify.phone","data":{"message":"` + shared + `Start um 06:00."}}]}`
	got := diffLines(t, before, after)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if !strings.Contains(got[0], "06:30") || !strings.Contains(got[0], "06:00") {
		t.Fatalf("divergence must stay visible in both cells: %q", got[0])
	}
	if !strings.Contains(got[0], "| …") {
		t.Fatalf("trimmed shared prefix must be marked with a leading …: %q", got[0])
	}
	// Short values keep their full text — no divergence trimming below the cap.
	got = diffLines(t, `{"mode":"single"}`, `{"mode":"restart"}`)
	assertLines(t, got, []string{"| Mode | single | restart |"})
}

func TestDiffEveryLineIsATableRow(t *testing.T) {
	// The structural invariant that keeps the whole output one continuous GFM
	// table: every emitted line — scalar, count, aligned item, honest cap,
	// notification copy — is a 3-cell data row.
	afterItems := make([]string, 0, 12)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		afterItems = append(afterItems, `{"action":"script.`+id+`"}`)
	}
	before := `{"mode":"single","description":"x","actions":[{"action":"notify.phone","data":{"message":"Plan erstellt"}}]}`
	after := `{"mode":"restart","actions":[{"action":"notify.phone","data":{"message":"Plan ist bereit"}},` + strings.Join(afterItems, ",") + `]}`
	got := diffLines(t, before, after)
	if len(got) < 5 {
		t.Fatalf("expected a mixed multi-row diff, got %#v", got)
	}
	for _, line := range got {
		if !strings.HasPrefix(line, "| ") || !strings.HasSuffix(line, " |") {
			t.Fatalf("not a table row: %q", line)
		}
		if n := unescapedPipes(line); n != 4 {
			t.Fatalf("row must have exactly 4 unescaped pipes, got %d: %q", n, line)
		}
	}
}

func TestDiffKeepsNormalNotificationMessagesUntruncated(t *testing.T) {
	before := `{"sequence":[{"action":"persistent_notification.create","data":{"message":"Das Script-Preview-Format wurde getestet."}}]}`
	after := `{"sequence":[{"action":"persistent_notification.create","data":{"message":"Das geänderte Script-Preview-Format wurde wirklich final getestet."}}]}`

	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Step 1 (data › message) | Das Script-Preview-Format wurde getestet. | Das geänderte Script-Preview-Format wurde wirklich final getestet. |",
	})
}

func TestDiffNumberRepresentationIsNotADrift(t *testing.T) {
	// HA can round-trip a config and change a number's form (5 -> 5.0, 1000 -> 1e3).
	// Mathematically-equal numbers must render NOTHING, so snapshot verify does not
	// cry false drift and block a safe revert (which would train users to ignore it).
	assertLines(t, diffLines(t, `{"max":5}`, `{"max":5.0}`), nil)
	assertLines(t, diffLines(t, `{"brightness":1000}`, `{"brightness":1e3}`), nil)
	assertLines(t, diffLines(t, `{"for":{"seconds":30}}`, `{"for":{"seconds":30.0}}`), nil)
	// A genuine numeric change is still a change.
	if got := diffLines(t, `{"max":5}`, `{"max":6}`); len(got) != 1 {
		t.Fatalf("expected 1 change for 5->6, got %#v", got)
	}
	// Large integers stay exact — no float-precision false match (the 1.x risk).
	if got := diffLines(t, `{"value":1700000000000000001}`, `{"value":1700000000000000002}`); len(got) != 1 {
		t.Fatalf("expected 1 change for distinct large ints, got %#v", got)
	}
}

func TestDecodeJSONNumberRejectsTrailingTokens(t *testing.T) {
	// A file with a second object / trailing garbage must fail, not silently compare
	// only the first object — diff or the revert drift check would otherwise operate
	// on a partial config (e.g. an LLM/file tool appends a second JSON object).
	for _, in := range []string{
		`{"mode":"restart"}{"mode":"single"}`,
		`{"mode":"restart"}` + "\n" + `{"mode":"single"}`,
		`{"mode":"restart"} garbage`,
		`{"mode":"restart"} 5`,
	} {
		if _, err := decodeJSONNumber([]byte(in)); err == nil {
			t.Fatalf("expected trailing-token rejection for %q", in)
		}
	}
	// A single object — including trailing whitespace/newline — still decodes cleanly.
	for _, in := range []string{`{"mode":"single"}`, `{"mode":"single"}` + "\n", "  {\"a\":1}  "} {
		if _, err := decodeJSONNumber([]byte(in)); err != nil {
			t.Fatalf("unexpected error for valid single object %q: %v", in, err)
		}
	}
}

func TestDiffTypeOnlyChangeShowsType(t *testing.T) {
	// A number→string (or bool→string) change must not render a confusing "5 → 5";
	// the type disambiguates it.
	assertLines(t, diffLines(t, `{"max":5}`, `{"max":"5"}`),
		[]string{"| Max | 5 (number) | 5 (string) |"})
	assertLines(t, diffLines(t, `{"on":true}`, `{"on":"true"}`),
		[]string{"| On | true (boolean) | true (string) |"})
}

func TestDiffMissingVsNonEmptyIsAChange(t *testing.T) {
	// A non-empty field appearing or disappearing IS a real change (not over-suppressed).
	assertLines(t, diffLines(t, `{"alias":"X"}`, `{"alias":"X","description":"real"}`),
		[]string{"| Description | — | real |"})
	if lines := diffLines(t, `{"alias":"X","triggers":[{"trigger":"time"}]}`, `{"alias":"X"}`); len(lines) != 1 {
		t.Fatalf("removed non-empty triggers should be one change, got %#v", lines)
	}
}

func TestDiffDurationChange(t *testing.T) {
	before := `{"mode":"single","actions":[{"action":"light.turn_on"},{"delay":{"minutes":5}},{"action":"light.turn_off"}]}`
	after := `{"mode":"single","actions":[{"action":"light.turn_on"},{"delay":{"minutes":2}},{"action":"light.turn_off"}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{"| Action 2 (delay) | 5 min | 2 min |"})
}

// Reproduces the live demo: a mode change AND a delay change, in stable order.
func TestDiffModeAndDelayInStableOrder(t *testing.T) {
	before := `{"mode":"single","actions":[{"action":"light.turn_on"},{"delay":{"minutes":5}},{"action":"light.turn_off"}]}`
	after := `{"mode":"restart","actions":[{"action":"light.turn_on"},{"delay":{"minutes":2}},{"action":"light.turn_off"}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Mode | single | restart |",
		"| Action 2 (delay) | 5 min | 2 min |",
	})
}

func TestDiffArrayLengthChange(t *testing.T) {
	got := diffLines(t, `{"triggers":[{"a":1}]}`, `{"triggers":[{"a":1},{"b":2}]}`)
	assertLines(t, got, []string{
		"| Triggers | 1 item | 2 items |",
		"| Trigger 2 | — | {\"b\":2} |",
	})
}

func TestDiffNestingActionsIntoIfBlockShowsWhatMoved(t *testing.T) {
	// Issue #274 problem 1: wrapping actions into one if-block REDUCES the
	// count — the diff must show what was removed and what block appeared,
	// or the count line actively misrepresents the change.
	before := `{"actions":[{"action":"script.a"},{"action":"script.b"},{"action":"script.c"},{"action":"script.d"},{"action":"script.e"}]}`
	after := `{"actions":[{"action":"script.a"},{"if":[{"condition":"state"}],"then":[{"action":"script.b"},{"action":"script.c"},{"action":"script.d"}]},{"action":"script.e"}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Actions | 5 items | 3 items |",
		"| Action 2 | action script.b | — |",
		"| Action 2 | — | if/then block (3 actions) |",
		"| Action 3 | action script.c | — |",
		"| Action 4 | action script.d | — |",
	})
}

func TestDiffAlignedItemsCapIsHonest(t *testing.T) {
	afterItems := make([]string, 0, 12)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		afterItems = append(afterItems, `{"action":"script.`+id+`"}`)
	}
	got := diffLines(t, `{"actions":[]}`, `{"actions":[`+strings.Join(afterItems, ",")+`]}`)
	if len(got) != 1+maxAlignedItemsPerSide+1 {
		t.Fatalf("expected header + %d items + honesty line, got %d lines: %#v", maxAlignedItemsPerSide, len(got), got)
	}
	if got[len(got)-1] != "| Actions | — | … and 4 more added |" {
		t.Fatalf("expected honest remainder line, got %q", got[len(got)-1])
	}
}

func TestDiffAlignedPairingKeepsFieldDiffForSameKind(t *testing.T) {
	// An edited item followed by an insertion pairs positionally with its
	// same-kind counterpart: field-level diff, not removed+added noise.
	before := `{"actions":[{"action":"light.turn_on","data":{"brightness":100}}]}`
	after := `{"actions":[{"action":"light.turn_on","data":{"brightness":50}},{"action":"script.extra"}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Actions | 1 item | 2 items |",
		"| Action 1 (data › brightness) | 100 | 50 |",
		"| Action 2 | — | action script.extra |",
	})
}

func TestDiffShowsNotificationCopyChangeEvenWhenActionsLengthChanges(t *testing.T) {
	before := `{"actions":[{"action":"notify.mobile_app_phone","data":{"title":"Nachtladung","message":"Plan erstellt","data":{"tag":"nachtladung"}}}]}`
	after := `{"actions":[{"action":"notify.mobile_app_phone","data":{"title":"Nachtladung","message":"Plan ist bereit","data":{"tag":"nachtladung"}}},{"action":"input_boolean.turn_on","target":{"entity_id":"input_boolean.nachtladung_plan_valid"}}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Actions | 1 item | 2 items |",
		"| Action 1 (data › message) | Plan erstellt | Plan ist bereit |",
		"| Action 2 | — | action input_boolean.turn_on |",
	})
}

func TestDiffShowsMovedNotificationCopyChangeWhenActionsLengthChanges(t *testing.T) {
	before := `{"actions":[{"action":"notify.mobile_app_phone","data":{"message":"Plan erstellt"}}]}`
	after := `{"actions":[{"action":"input_boolean.turn_on","target":{"entity_id":"input_boolean.nachtladung_plan_valid"}},{"action":"notify.mobile_app_phone","data":{"message":"Plan ist bereit"}}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Actions | 1 item | 2 items |",
		"| Action 1 | action notify.mobile_app_phone | — |",
		"| Action 1 | — | action input_boolean.turn_on |",
		"| Action 2 | — | action notify.mobile_app_phone |",
		"| Action 2 (data › message) | Plan erstellt | Plan ist bereit |",
	})
}

func TestDiffAddAndRemoveKey(t *testing.T) {
	added := diffLines(t, `{"mode":"single"}`, `{"mode":"single","max":3}`)
	assertLines(t, added, []string{"| Max | — | 3 |"})

	removed := diffLines(t, `{"mode":"single","max":3}`, `{"mode":"single"}`)
	assertLines(t, removed, []string{"| Max | 3 | — |"})
}

func TestDiffBooleanChange(t *testing.T) {
	got := diffLines(t, `{"enabled":true}`, `{"enabled":false}`)
	assertLines(t, got, []string{"| Enabled | true | false |"})
}

func TestDiffPluralAliasIsNotAChange(t *testing.T) {
	// Singular `trigger` vs stored plural `triggers`, identical content → no diff.
	got := diffLines(t,
		`{"trigger":[{"platform":"state","entity_id":"x"}],"mode":"single"}`,
		`{"triggers":[{"platform":"state","entity_id":"x"}],"mode":"single"}`)
	assertLines(t, got, nil)
}

func TestDiffPluralAliasStillDetectsRealChange(t *testing.T) {
	got := diffLines(t, `{"trigger":[{"to":"on"}]}`, `{"triggers":[{"to":"off"}]}`)
	assertLines(t, got, []string{"| Trigger 1 (to) | on | off |"})
}

func TestDiffSingularObjectVsPluralListIsNotAChange(t *testing.T) {
	// HA accepts a single trigger as an object (`trigger: {…}`) and stores it as a
	// one-item plural list (`triggers: [{…}]`). The two forms must compare equal,
	// or snapshot verify would cry false drift and block a safe revert.
	assertLines(t, diffLines(t,
		`{"trigger":{"platform":"state","entity_id":"x"},"mode":"single"}`,
		`{"triggers":[{"platform":"state","entity_id":"x"}],"mode":"single"}`), nil)
	assertLines(t, diffLines(t,
		`{"condition":{"condition":"state","state":"on"}}`,
		`{"conditions":[{"condition":"state","state":"on"}]}`), nil)
	// A real difference inside the wrapped object is still detected.
	if got := diffLines(t, `{"trigger":{"to":"on"}}`, `{"triggers":[{"to":"off"}]}`); len(got) != 1 {
		t.Fatalf("expected 1 change for trigger to on->off, got %#v", got)
	}
}

func TestDiffIgnoresMetadata(t *testing.T) {
	got := diffLines(t,
		`{"id":"1700000000000","unique_id":"1700000000000","mode":"single"}`,
		`{"id":"1700000000001","unique_id":"1700000000001","mode":"single"}`)
	assertLines(t, got, nil)
}

func TestDiffNoChange(t *testing.T) {
	cfg := `{"alias":"X","mode":"single","triggers":[{"platform":"state"}]}`
	got := diffLines(t, cfg, cfg)
	assertLines(t, got, nil)
}

func TestDiffStableTopLevelOrdering(t *testing.T) {
	// alias (0) before mode (2) before a non-priority key (alphabetical).
	before := `{"mode":"single","alias":"Old","zone":"home"}`
	after := `{"mode":"restart","alias":"New","zone":"away"}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Alias | Old | New |",
		"| Mode | single | restart |",
		"| Zone | home | away |",
	})
}

func TestDiffAnchorsDeepNestingOnTheRecognizableItem(t *testing.T) {
	// The real-world case that motivated anchors: a threshold buried in
	// choose -> or -> and -> numeric_state. The label must name the entity,
	// not the index walk "choose 1 › condition 2 › condition 2 › below".
	before := `{"actions":[{"choose":[{"conditions":[{"condition":"trigger","id":"x"},{"condition":"or","conditions":[{"condition":"state","entity_id":"input_boolean.darkness","state":"on"},{"condition":"and","conditions":[{"condition":"state","entity_id":"light.diele","state":"off"},{"below":10,"condition":"numeric_state","entity_id":"sensor.diele_illumination"}]}]}],"sequence":[{"action":"light.turn_on"}]}]}]}`
	after := strings.Replace(before, `"below":10`, `"below":20`, 1)
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Action 1 (choose 1 › condition sensor.diele_illumination › below) | 10 | 20 |",
	})
}

func TestDiffAnchorPrefersAliasAndKeepsIndexFallback(t *testing.T) {
	// A user-named step anchors on its alias.
	before := `{"actions":[{"choose":[{"conditions":[{"condition":"state","entity_id":"x","state":"on"}],"sequence":[{"alias":"Lampe an","action":"light.turn_on","data":{"brightness":100}}]}]}]}`
	after := strings.Replace(before, `"brightness":100`, `"brightness":50`, 1)
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		`| Action 1 (choose 1 › "Lampe an" › data › brightness) | 100 | 50 |`,
	})
	// Unknown shapes keep today's deterministic index fallback.
	before = `{"actions":[{"custom":[{"weird":1},{"weird":2}]}]}`
	after = `{"actions":[{"custom":[{"weird":1},{"weird":3}]}]}`
	got = diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Action 1 (custom 2 › weird) | 2 | 3 |",
	})
}

func TestDiffNestedNotificationCopyDedupsWithAnchoredLabel(t *testing.T) {
	// The aligned pass and the notification-copy pass must build IDENTICAL
	// anchored labels, or the dedup (path+text) silently stops working for
	// nested sequences and one copy change renders twice.
	before := `{"actions":[{"choose":[{"conditions":[{"condition":"state","entity_id":"x","state":"on"}],"sequence":[{"action":"notify.phone","data":{"message":"Plan erstellt"}}]}]}]}`
	after := `{"actions":[{"choose":[{"conditions":[{"condition":"state","entity_id":"x","state":"on"}],"sequence":[{"action":"notify.phone","data":{"message":"Plan ist bereit"}},{"action":"script.extra"}]}]}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Action 1 (choose 1 › sequence) | 1 item | 2 items |",
		"| Action 1 (choose 1 › action notify.phone › data › message) | Plan erstellt | Plan ist bereit |",
		"| Action 1 (choose 1 › step 2) | — | action script.extra |",
	})
}

func TestDiffFieldColumnHasItsOwnWiderCap(t *testing.T) {
	// An anchored label (branch + entity + field) may exceed the 80-byte value
	// cap; cutting the FIELD mid-name would lose what identifies the row, so
	// the field column caps at 120 — and beyond that still cuts rune-safe.
	entity := "sensor." + strings.Repeat("diele_praesenzmelder_", 2) + "illumination" // label ~100 bytes: over the 80 value cap, under the 120 field cap
	before := `{"actions":[{"choose":[{"conditions":[{"below":10,"condition":"numeric_state","entity_id":"` + entity + `"}],"sequence":[{"action":"script.a"}]}]}]}`
	after := strings.Replace(before, `"below":10`, `"below":20`, 1)
	got := diffLines(t, before, after)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %#v", got)
	}
	if !strings.Contains(got[0], "› below) | 10 | 20 |") {
		t.Fatalf("field name must survive whole under the wider field cap: %q", got[0])
	}
}

func TestDiffAnchorCollisionsKeepBranchContext(t *testing.T) {
	// Two identical conditions in two choose branches must stay
	// distinguishable — the branch token survives the anchor reset.
	before := `{"actions":[{"choose":[{"conditions":[{"below":10,"condition":"numeric_state","entity_id":"sensor.x"}],"sequence":[{"action":"script.a"}]},{"conditions":[{"below":30,"condition":"numeric_state","entity_id":"sensor.x"}],"sequence":[{"action":"script.b"}]}]}]}`
	after := strings.Replace(strings.Replace(before, `"below":10`, `"below":15`, 1), `"below":30`, `"below":35`, 1)
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Action 1 (choose 1 › condition sensor.x › below) | 10 | 15 |",
		"| Action 1 (choose 2 › condition sensor.x › below) | 30 | 35 |",
	})
}

func TestDiffPayloadArraysNeverAnchor(t *testing.T) {
	// Actionable-notification buttons live in data.actions[] and carry their
	// own action/title keys — anchoring them would mislabel a button as a
	// service call. Payload subtrees keep the index fallback.
	before := `{"actions":[{"action":"notify.phone","data":{"message":"Tür","actions":[{"action":"OPEN","title":"Öffnen"},{"action":"IGNORE","title":"Ignorieren"}]}}]}`
	after := strings.Replace(before, `"title":"Ignorieren"`, `"title":"Später"`, 1)
	got := diffLines(t, before, after)
	assertLines(t, got, []string{
		"| Action 1 (data › action 2 › title) | Ignorieren | Später |",
	})
}

func TestDiffNestedConditionChange(t *testing.T) {
	before := `{"conditions":[{"condition":"numeric_state","above":60}]}`
	after := `{"conditions":[{"condition":"numeric_state","above":40}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{"| Condition 1 (above) | 60 | 40 |"})
}

func TestDiffMultiUnitDuration(t *testing.T) {
	before := `{"actions":[{"delay":{"hours":1,"minutes":30}}]}`
	after := `{"actions":[{"delay":{"minutes":45}}]}`
	got := diffLines(t, before, after)
	assertLines(t, got, []string{"| Action 1 (delay) | 1 h 30 min | 45 min |"})
}
