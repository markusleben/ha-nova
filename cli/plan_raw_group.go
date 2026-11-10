package main

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A raw plan's steps as its confirmation says them (rawSayWhat for the
// window, the form, and the chat; rawAsk for the phone), short enough to
// read at a glance: steps in a row that do the same share one line
// (rawRuns), and a statistics import says its figures in plain words, with
// the statistic's name and months instead of ids and timestamps. The
// overwrite of stored hours is said once, by the typed reason
// (raw.why.statistics_import). Every step stays one item in the result
// (rawPlanResult.Items), with its own line, for the details.

// rawImport: a statistics import's figures.
type rawImport struct {
	id          string
	name        string // the statistic's entity's name, else its own, else its id
	rows        int
	first, last string // the first and the last row's start, as sent
}

func (n *rawNamer) statsImport(endpoint string, payload map[string]any) *rawImport {
	if endpoint != "ws" || payload["type"] != "recorder/import_statistics" {
		return nil
	}
	metadata, _ := payload["metadata"].(map[string]any)
	rows, _ := payload["stats"].([]any)
	id, _ := metadata["statistic_id"].(string)
	title, _ := metadata["name"].(string)
	imp := &rawImport{id: id, rows: len(rows)}
	if strings.Contains(id, ":") { // an external statistic has no entity
		imp.name = firstNonEmpty(sayText(title, true), sayText(id, true))
	} else {
		imp.name = n.entity(id)
	}
	if len(rows) > 0 {
		firstRow, _ := rows[0].(map[string]any)
		lastRow, _ := rows[len(rows)-1].(map[string]any)
		imp.first, _ = firstRow["start"].(string)
		imp.last, _ = lastRow["start"].(string)
	}
	return imp
}

func (imp *rawImport) object(lang string) string {
	if imp.rows == 0 {
		return confirmT(lang, "raw.object.import", "statistic", imp.name)
	}
	return confirmT(lang, "raw.object.import_span", "count", imp.rows, "statistic", imp.name, "span", rawSpan(lang, imp.first, imp.last))
}

// dated: rows whose times read, so a shared line can say its months.
func (imp *rawImport) dated() bool {
	_, first := time.Parse(time.RFC3339, imp.first)
	_, last := time.Parse(time.RFC3339, imp.last)
	return imp.rows > 0 && first == nil && last == nil
}

// rawSpan: the months from first to last (de „Nov. 2024 – Jan. 2026“), in
// the time zone the rows were sent in; a time that doesn't read stays as
// sent.
func rawSpan(lang, first, last string) string {
	month := func(start string) string {
		t, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return sayText(start, true)
		}
		return strings.Fields(confirmT(lang, "date.months"))[t.Month()-1] + " " + strconv.Itoa(t.Year())
	}
	if from, to := month(first), month(last); from != to {
		return confirmT(lang, "date.range", "first", from, "last", to)
	}
	return month(first)
}

// rawAlike: whether b shares a's line. Both are plain requests of one kind
// and tier (nothing of their own to add: a removal note, a device's
// entities, a detail), and both import dated statistics or both delete (or
// both remove) one item. Anything else keeps its own line.
func rawAlike(a, b rawPlanItem) bool {
	switch {
	case a.kind != b.kind || a.Tier != b.Tier || a.drops+a.more+a.detail+b.drops+b.more+b.detail != "":
		return false
	case a.stats != nil:
		return a.stats.dated() && b.stats.dated()
	}
	return (a.goal.raw == "delete" || a.goal.raw == "remove") && b.goal.raw == a.goal.raw && len(a.goal.names) == 1 && len(b.goal.names) == 1
}

// rawRuns: the steps in order, those alike in a row together.
func rawRuns(steps []rawPlanItem) (runs [][]rawPlanItem) {
	for i := 0; i < len(steps); {
		j := i + 1
		for j < len(steps) && rawAlike(steps[i], steps[j]) {
			j++
		}
		runs, i = append(runs, steps[i:j]), j
	}
	return runs
}

// rawRunLine: a run's line. Deletes name every item; imports name every
// statistic, or (all false, for the phone) only their figures.
func rawRunLine(lang string, run []rawPlanItem, all bool) string {
	first := run[0]
	if len(run) == 1 {
		return upperFirst(first.line)
	}
	names, total, span := rawRunNames(lang, run)
	if first.stats == nil {
		return upperFirst(actionGoal{raw: first.goal.raw}.phrase(lang, sayAll(lang, names)))
	}
	if !all {
		return upperFirst(confirmT(lang, "raw.import_count", "values", total, "count", len(names), "span", span))
	}
	return upperFirst(confirmT(lang, "raw.import_group", "values", total, "count", len(names), "span", span, "names", sayAll(lang, names)))
}

// rawRunNames: what a run of alike steps acts on: the item each deletes, or
// each statistic it imports into, once, with the values in all (as the
// language writes the number) and their months.
func rawRunNames(lang string, run []rawPlanItem) (names []string, total, span string) {
	first := run[0]
	if first.stats == nil {
		for _, item := range run {
			names = append(names, item.goal.names[0])
		}
		return names, "", ""
	}
	values, from, to := 0, first.stats.first, first.stats.last
	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
	seen := map[string]bool{}
	for _, item := range run {
		if !seen[item.stats.id] { // two imports into one statistic name it once
			seen[item.stats.id], names = true, append(names, item.stats.name)
		}
		values += item.stats.rows
		if at(item.stats.first).Before(at(from)) {
			from = item.stats.first
		}
		if at(item.stats.last).After(at(to)) {
			to = item.stats.last
		}
	}
	total = strconv.Itoa(values)
	for i := len(total) - 3; i > 0; i -= 3 {
		total = total[:i] + confirmT(lang, "number.thousands") + total[i:]
	}
	return names, total, rawSpan(lang, from, to)
}

// rawSayWhat opens a raw plan's confirmation with what happens in the home:
// one line per run of steps, in order, each with its detail, and the
// copies saved first.
func rawSayWhat(lang string, env *verbEnvelope) (about, status string) {
	steps, copies := rawSteps(env)
	detail := func(item rawPlanItem) (say string) {
		if item.drops != "" {
			say = " " + item.drops
		}
		if item.detail != "" {
			say += " " + confirmT(lang, "detail.say", "detail", item.detail)
		}
		return say
	}
	copied := rawCopyNote(lang, copies)
	if len(steps) == 1 {
		return strings.TrimSpace(sayAbout(lang, steps[0].line) + detail(steps[0]) + " " + copied), sayNotRun(lang)
	}
	lines := []string{confirmT(lang, "raw.steps_intro", "count", len(steps))}
	if copied != "" {
		lines = append(lines, copied)
	}
	number := 1
	for _, run := range rawRuns(steps) {
		if len(run) == 1 {
			lines = append(lines, confirmT(lang, "raw.step_line", "number", number, "change", run[0].Change)+detail(run[0]))
		} else {
			lines = append(lines, confirmT(lang, "raw.steps_line", "first", number, "last", number+len(run)-1, "change", rawRunLine(lang, run, true)))
		}
		number += len(run)
	}
	return strings.Join(lines, "\n"), sayNotRun(lang)
}

// rawAsk: a typed raw plan's phone question. With several items the
// notification names the typed steps first, by number (steps alike in a row
// on one line), then the reasons, then how many other steps come along.
// Too long for the notification, the steps are cut, never the reasons.
func rawAsk(e *tierEngine, env *verbEnvelope, plan storedPlan) actionGoal {
	lang := textLang(plan)
	goal := rawGoal(lang, env)
	steps, _ := rawSteps(env)
	var why []string
	if len(steps) == 1 && steps[0].more != "" { // what completes the title
		why = append(why, upperFirst(steps[0].more))
	}
	for _, item := range steps {
		if item.drops != "" {
			why = append(why, strings.TrimSuffix(item.drops, "."))
		}
	}
	var named, tail []string
	typed, number := 0, 1
	for _, run := range rawRuns(steps) {
		switch {
		case len(steps) == 1 || run[0].Tier != tierTyped.String():
		case len(run) == 1:
			named = append(named, confirmT(lang, "raw.step_why", "number", number, "change", run[0].Change))
		default:
			named = append(named, confirmT(lang, "raw.steps_why", "first", number, "last", number+len(run)-1, "change", rawRunLine(lang, run, false)))
		}
		if len(steps) > 1 && run[0].Tier == tierTyped.String() {
			typed += len(run)
		}
		number += len(run)
	}
	reasons := map[string]bool{}
	for _, r := range env.Reasons {
		if text, typed := sayReason(lang, e, env, plan, r); typed && text != "" && !reasons[text] {
			reasons[text] = true
			tail = append(tail, text)
		}
	}
	if others := len(steps) - typed; len(steps) > 1 && others > 0 {
		tail = append(tail, confirmT(lang, "raw.others", "count", others))
	}
	if all := strings.Join(append(append(slices.Clone(why), named...), tail...), ". "); len(named) > 0 && utf8.RuneCountInString(all) > phoneWhyLimit {
		more := map[bool]string{true: confirmT(lang, "phone.more_in_chat"), false: confirmT(lang, "phone.more_on_computer")}[chatCall != nil]
		room := phoneWhyLimit - utf8.RuneCountInString(strings.Join(append(slices.Clone(why), tail...), ". ")) - utf8.RuneCountInString(more) - 4
		named = []string{phoneCut(strings.Join(named, ". "), max(room, phoneNameRoom)) + " " + more}
	}
	why = append(append(why, named...), tail...)
	goal.why = strings.Join(why, ". ")
	return goal
}
