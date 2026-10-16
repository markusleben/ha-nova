package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// First look after pairing (docs/work/2026-10-16-ha-sign-in-pairing-spec.md,
// "First look"; contract: home-map spec §4). Read-only and bounded; an error,
// a timeout, or a panic while building it skips it, so it never fails setup.

var setupFirstLookBudget = 5 * time.Second

// setupFirstLook is the map's first look plus the setup-only safety example
// (kept out of `ha-nova map`, so skills never turn it into an everyday prompt).
type setupFirstLook struct {
	firstLookResult
	Lock *firstSeed
}

var setupFirstLookRead = func(paths runtimePaths) (setupFirstLook, error) {
	s, err := newRelaySessionWithin(paths, verbOptions{}, setupFirstLookBudget)
	if err != nil {
		return setupFirstLook{}, err
	}
	defer s.close()
	home, err := loadHome(s)
	if err != nil {
		return setupFirstLook{}, err
	}
	x := indexHome(home, time.Now())
	look := setupFirstLook{firstLookResult: x.firstLook()}
	if seed, ok := x.codeSeed(); ok {
		look.Lock = &seed
	}
	return look, nil
}

// codeCoverClasses: covers that open a way in; opening one always needs
// HA NOVA's one-time code (accessCapable in tier_rules.go).
var codeCoverClasses = map[string]bool{"garage": true, "gate": true, "door": true}

// codeSeed: something HA NOVA opens only after its one-time code — a lock
// without its own keypad code (those are handed to the Home Assistant UI,
// directRefusal), else a garage door, gate, or door cover.
func (x *homeIndex) codeSeed() (firstSeed, bool) {
	area, _ := x.seedArea()
	if seed, ok := x.pickSeed(area.AreaID, func(id string, st *mapState) bool {
		format, _ := st.Attributes.CodeFormat.(string)
		return inDomain(id, "lock") && format == ""
	}); ok {
		return seed, true
	}
	return x.pickSeed(area.AreaID, func(id string, st *mapState) bool {
		return inDomain(id, "cover") && !st.isGroup() && codeCoverClasses[st.deviceClass()]
	})
}

// loadSetupFirstLook returns nil when the first look is not available. A
// read that outlives the backstop is abandoned; setup exits right after.
func loadSetupFirstLook(out io.Writer, paths runtimePaths) *setupFirstLook {
	done := make(chan *setupFirstLook, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- nil
			}
		}()
		look, err := setupFirstLookRead(paths)
		if err != nil {
			done <- nil
			return
		}
		done <- &look
	}()
	// The session budget bounds every read; this backstop also covers
	// config and credential loading.
	wait := func() (*setupFirstLook, error) {
		select {
		case look := <-done:
			return look, nil
		case <-time.After(setupFirstLookBudget + time.Second):
			return nil, nil
		}
	}
	if !resolveSetupUISession(out).animatesSpinner() {
		look, _ := wait()
		return look
	}
	look, _ := runSetupSpinnerWithResult(out, "Taking a first look at your home...", setupSpinnerDebounce, wait)
	return look
}

// renderSetupFirstLook prints the home line, observations, example prompts
// under promptHeader, and the safety line.
func renderSetupFirstLook(out io.Writer, look *setupFirstLook, promptHeader string) {
	if line := firstLookHomeLine(look.Home); line != "" {
		fmt.Fprintf(out, "  %s\n\n", line)
	}
	var notes []string
	for _, f := range look.Observations {
		if note := firstLookNote(f); note != "" {
			notes = append(notes, note)
		}
	}
	if len(notes) > 0 {
		fmt.Fprintln(out, "  Worth a look:")
		for _, note := range notes[:min(len(notes), 2)] {
			fmt.Fprintf(out, "    - %s\n", note)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  %s\n\n", promptHeader)
	for _, prompt := range firstLookPrompts(look.Seeds) {
		fmt.Fprintf(out, "    \"%s\"\n", prompt)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n\n", firstLookSafetyLine(look.Lock))
}

func firstLookHomeLine(home map[string]int) string {
	var parts []string
	for _, c := range []struct{ key, noun string }{{"areas", "room"}, {"devices", "device"}, {"automations", "automation"}} {
		if n := home[c.key]; n > 0 {
			parts = append(parts, plural(n, c.noun))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Your home: " + strings.Join(parts, ", ") + "."
}

// firstLookName: a name safe for the terminal, or "" for anything that must
// not be shown (presence domains never appear).
func firstLookName(entityID, name string) string {
	if domain, _, ok := strings.Cut(entityID, "."); ok && presenceDomains[domain] {
		return ""
	}
	return terminalSafeName(name)
}

// seedName: like firstLookName, but never a bare entity_id (the map falls
// back to it when an entity has no name).
func seedName(s firstSeed) string {
	if name := firstLookName(s.EntityID, s.Name); !entityIDPattern.MatchString(name) {
		return name
	}
	return ""
}

func firstLookNote(f firstLookFinding) string {
	name, area := firstLookName(f.EntityID, f.Name), terminalSafeName(f.Area)
	if area != "" {
		area = " (" + area + ")"
	}
	more := ""
	if f.Count > 1 {
		more = fmt.Sprintf(" (and %d more)", f.Count-1)
	}
	switch {
	case f.Kind == "integration_problem": // never the domain or entry title
		return fmt.Sprintf("An integration %s%s.", integrationProblemWords(f.State), more)
	case name == "":
	case f.Kind == "battery_low":
		level := "is low"
		if value := terminalSafeName(f.Value); strings.HasSuffix(value, "%") {
			level = "is at " + value
		}
		return fmt.Sprintf("Low battery: %s%s %s%s.", name, area, level, more)
	case f.Kind == "automation_unavailable":
		return fmt.Sprintf("The automation \"%s\" can't run right now%s.", name, more)
	case f.Kind == "automation_never_triggered":
		return fmt.Sprintf("The automation \"%s\" has never run%s.", name, more)
	case f.Kind == "unavailable_long": // the age is a lower bound
		since := "a week"
		if days, ok := strings.CutSuffix(f.Value, "d"); ok && days != "" && strings.Trim(days, "0123456789") == "" {
			since = days + " days"
		}
		return fmt.Sprintf("%s%s has not reported anything for at least %s%s.", name, area, since, more)
	}
	return ""
}

func integrationProblemWords(state string) string {
	switch state {
	case "setup_error":
		return "couldn't start"
	case "setup_retry":
		return "keeps trying to connect"
	case "not_loaded":
		return "isn't running"
	}
	return "needs attention"
}

// firstLookPrompts: one prompt per seed present, at most three, only real
// names; without seeds, prompts that name nothing.
func firstLookPrompts(seeds map[string]firstSeed) []string {
	var prompts []string
	add := func(key string, build func(name, area string, s firstSeed) string) {
		s, ok := seeds[key]
		if name := seedName(s); ok && name != "" && len(prompts) < 3 {
			prompts = append(prompts, build(name, terminalSafeName(s.Area), s))
		}
	}
	add("light", func(name, _ string, _ firstSeed) string { return "Turn on " + name })
	add("climate", func(name, area string, s firstSeed) string {
		switch {
		case inDomain(s.EntityID, "climate"):
			return "Make " + name + " a bit warmer"
		case area != "":
			return "How warm is it in " + area + "?"
		}
		return "What does " + name + " show right now?"
	})
	add("automation", func(name, _ string, _ firstSeed) string {
		return "Why did the automation '" + name + "' run last time?"
	})
	add("cover", func(name, _ string, _ firstSeed) string { return "Close " + name })
	add("area", func(name, _ string, _ firstSeed) string { return "What's on in " + name + "?" })
	if len(prompts) == 0 {
		return []string{"What's in my home?", "List my automations"}
	}
	return prompts
}

// firstLookSafetyLine states the code step; it never invites a real unlock.
func firstLookSafetyLine(lock *firstSeed) string {
	if lock != nil {
		if name := seedName(*lock); name != "" {
			verb := "open"
			if inDomain(lock.EntityID, "lock") {
				verb = "unlock"
			}
			return fmt.Sprintf("Ask to %s %s and HA NOVA first shows you a one-time code; nothing happens until you type it back.", verb, name)
		}
	}
	return "HA NOVA never unlocks a lock or opens a gate or garage door until you type back a one-time code it shows you."
}
