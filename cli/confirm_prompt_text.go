package main

import "strings"

// A typed plan's window and form text (confirmationPrompt), read at a
// glance (docs/work/2026-11-10-wider-confirm-window-spec.md): the action as
// one question, then what it acts on, one line each. Every name stays in
// full, never "N more": the window budget refuses a plan that doesn't fit.

// promptWhat: the question and what it acts on ("" when the question says
// it all).
func promptWhat(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) (title, what string) {
	lang := textLang(plan)
	if plan.Kind == "raw" {
		return rawPrompt(lang, env)
	}
	goal := sayGoal(e, env, plan, subject)
	if plan.ConfigAction != nil {
		return promptQuestion(lang, goal.text(lang)), ""
	}
	about, _ := sayWhat(e, env, plan, subject)
	if len(goal.names) < 2 {
		return promptQuestion(lang, goal.text(lang)), about
	}
	these := goal
	if goal.frame == "" { // the verb says what they are ("Unlock these 3?"); a frame keeps its kind and the first names
		these.names = []string{confirmT(lang, "window.these", "count", len(goal.names))}
	}
	if env.everyday != nil { // its own line says what happens
		return promptQuestion(lang, these.text(lang)), about
	}
	return promptQuestion(lang, sayWithData(lang, these.text(lang), plan.Action.Data)), promptBullets(lang, goal.names)
}

// rawPrompt: a raw plan's question and steps. Steps that are all alike (one
// run: statistics imports, or deletes) are one question with a line per
// statistic or item; any other plan keeps its numbered lines, each with its
// detail.
func rawPrompt(lang string, env *verbEnvelope) (title, what string) {
	steps, copies := rawSteps(env)
	runs := rawRuns(steps)
	if len(steps) == 1 || len(runs) > 1 {
		about, _ := rawSayWhat(lang, env)
		lines := strings.Split(about, "\n")
		if len(steps) > 1 { // the intro: the question says it
			lines = lines[1:]
		}
		return promptQuestion(lang, rawGoal(lang, env).text(lang)), strings.Join(lines, "\n")
	}
	var lines []string
	if copied := rawCopyNote(lang, copies); copied != "" {
		lines = append(lines, copied)
	}
	names, total, span := rawRunNames(lang, runs[0])
	if steps[0].stats != nil {
		title = confirmT(lang, "raw.import_title", "values", total, "count", len(names))
		lines = append(lines, confirmT(lang, "raw.span", "span", span))
	} else {
		title = actionGoal{raw: steps[0].goal.raw}.phrase(lang, sayList(lang, names)) // the names say what they are
	}
	return promptQuestion(lang, title), strings.Join(append(lines, promptBullets(lang, names)), "\n")
}

// promptQuestion: "Unlock “Front Door”?"
func promptQuestion(lang, action string) string {
	return confirmT(lang, "phone.title", "goal", upperFirst(action))
}

// promptBullets: one line per name, each as sayText quoted it.
func promptBullets(lang string, names []string) string {
	lines := make([]string, len(names))
	for i, name := range names {
		lines[i] = confirmT(lang, "window.bullet", "name", name)
	}
	return strings.Join(lines, "\n")
}
