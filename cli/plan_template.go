package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Automation templates for setup step 4 (onboarding 0.27 spec §7.5): a
// fixed, pre-reviewed body per template, filled only with the home's real
// entities. "ha-nova plan template <id>" stores it as a normal automation
// create plan; "ha-nova apply" saves it. Never saved without that apply.

const planTemplateUsage = "ha-nova plan template door_light_after_dark|lights_off_when_away|battery_reminder"

const (
	templateDoorLight       = "door_light_after_dark"
	templateLightsOff       = "lights_off_when_away"
	templateBatteryReminder = "battery_reminder"
	reminderBelow           = 10
	lightsOffMax            = 30 // ponytail: a bigger home gets no away template; per-area targets if users ask
)

// automationTemplates in pick order: the first match is "Made for your home".
var automationTemplates = []string{templateDoorLight, templateLightsOff, templateBatteryReminder}

type templateMatch struct {
	Template string        `json:"template"`
	Uses     []templateUse `json:"uses"`
	draft    automationDraft
}

// templateUse: a real entity the draft uses, by role (trigger, light,
// battery, phone). Persons, trackers, and zones are never listed.
type templateUse struct {
	Role     string `json:"role"`
	EntityID string `json:"entity_id,omitempty"`
	Name     string `json:"name"`
}

// automationDraft keeps the key order Home Assistant's editor shows.
type automationDraft struct {
	Alias       string `json:"alias"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Triggers    []any  `json:"triggers"`
	Conditions  []any  `json:"conditions"`
	Actions     []any  `json:"actions"`
}

const templateDescription = "Made by HA NOVA setup from a pre-reviewed template."

func (x *homeIndex) matchTemplate(id string, phone *phoneTarget) (templateMatch, bool) {
	switch id {
	case templateDoorLight:
		return x.doorLight()
	case templateLightsOff:
		return x.lightsOff()
	case templateBatteryReminder:
		return x.batteryReminder(phone)
	}
	return templateMatch{}, false
}

// templateEntity: a visible, available, single entity (no group, no config
// or diagnostic entity).
func (x *homeIndex) templateEntity(id string) bool {
	st := x.state[id]
	return st != nil && st.State != "unavailable" && st.State != "unknown" && !x.hidden[id] && x.category[id] == "" && !st.isGroup()
}

func (x *homeIndex) templateLights() []string {
	var lights []string
	private := x.privateDevices()
	for _, id := range x.enabled { // sorted
		if inDomain(id, "light") && x.templateEntity(id) && !private[x.entityDevice[id]] {
			lights = append(lights, id)
		}
	}
	return lights
}

// pairLight: the first light in the trigger's own area, else "". Names are
// never matched: friendly names carry brand words (IKEA, Hue) that would
// pair a door with a lamp in another room.
func (x *homeIndex) pairLight(trigger string, lights []string) string {
	if area := x.entityArea[trigger]; area != "" {
		for _, l := range lights {
			if x.entityArea[l] == area {
				return l
			}
		}
	}
	return ""
}

// doorLight: a lock that is unlocked, or a door that opens, after dark
// turns on a light in the same area. A car's lock or door (a device with a
// tracker) is never a trigger. State triggers name from: so a return from
// unavailable never fires, and a lock's unlocking step is in from: (#743).
func (x *homeIndex) doorLight() (templateMatch, bool) {
	var locks, doors []string
	private := x.privateDevices()
	for _, id := range x.enabled {
		if !x.templateEntity(id) || private[x.entityDevice[id]] {
			continue
		}
		switch class := x.state[id].deviceClass(); {
		case inDomain(id, "lock"):
			locks = append(locks, id)
		case inDomain(id, "binary_sensor") && (class == "door" || class == "garage_door"):
			doors = append(doors, id)
		}
	}
	lights := x.templateLights()
	for _, trigger := range append(locks, doors...) {
		light := x.pairLight(trigger, lights)
		if light == "" {
			continue
		}
		name := x.name(trigger)
		t := map[string]any{"trigger": "state", "entity_id": trigger, "from": "off", "to": "on"}
		alias := "Light on when " + name + " opens after dark"
		if inDomain(trigger, "lock") {
			t = map[string]any{"trigger": "state", "entity_id": trigger, "from": []string{"locked", "unlocking"}, "to": "unlocked"}
			alias = "Light on when " + name + " is unlocked after dark"
		}
		dark := map[string]any{"condition": "or", "conditions": []any{
			map[string]any{"condition": "sun", "after": "sunset"}, map[string]any{"condition": "sun", "before": "sunrise"}}}
		return templateMatch{Template: templateDoorLight,
			Uses: []templateUse{{Role: "trigger", EntityID: trigger, Name: name}, {Role: "light", EntityID: light, Name: x.name(light)}},
			draft: automationDraft{Alias: alias, Description: templateDescription, Mode: "single", Triggers: []any{t}, Conditions: []any{dark},
				Actions: []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": light}}}}}, true
	}
	return templateMatch{}, false
}

// lightsOff: when nobody has been home for 10 minutes (zone.home counts the
// persons in it), turn the lights off. Needs a person whose location is
// known (one stuck at unknown keeps zone.home at 0, so it never fires) and
// a numeric zone.home; neither is ever listed.
func (x *homeIndex) lightsOff() (templateMatch, bool) {
	home, persons := x.state["zone.home"], 0
	for _, id := range x.enabled {
		if st := x.state[id]; inDomain(id, "person") && st != nil && st.State != "unknown" && st.State != "unavailable" {
			persons++
		}
	}
	lights := x.templateLights()
	if home == nil || persons == 0 || len(lights) == 0 || len(lights) > lightsOffMax {
		return templateMatch{}, false
	}
	if _, err := strconv.ParseFloat(home.State, 64); err != nil {
		return templateMatch{}, false
	}
	m := templateMatch{Template: templateLightsOff, draft: automationDraft{Alias: "Lights off when everyone has left", Description: templateDescription, Mode: "single",
		Triggers:   []any{map[string]any{"trigger": "numeric_state", "entity_id": "zone.home", "below": 1, "for": map[string]any{"minutes": 10}}},
		Conditions: []any{}, Actions: []any{map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": lights}}}}}
	for _, l := range lights {
		m.Uses = append(m.Uses, templateUse{Role: "light", EntityID: l, Name: x.name(l)})
	}
	return m, true
}

// batteryReminder: a device battery (a numeric level, not a phone or a
// car) dropping below 10% sends one notification to the linked phone.
func (x *homeIndex) batteryReminder(phone *phoneTarget) (templateMatch, bool) {
	if phone == nil || !phoneServicePattern.MatchString(phone.Service) {
		return templateMatch{}, false
	}
	_, rows := x.batteries()
	m := templateMatch{Template: templateBatteryReminder}
	var batteries []string
	private := x.privateDevices()
	for _, row := range rows {
		if row.Kind == "device" && strings.HasSuffix(row.Level, "%") && !private[x.entityDevice[row.EntityID]] {
			batteries = append(batteries, row.EntityID)
			m.Uses = append(m.Uses, templateUse{Role: "battery", EntityID: row.EntityID, Name: row.Name})
		}
	}
	if len(batteries) == 0 {
		return templateMatch{}, false
	}
	slices.Sort(batteries)
	m.Uses = append(m.Uses, templateUse{Role: "phone", Name: phone.Name})
	m.draft = automationDraft{Alias: fmt.Sprintf("Remind me when a battery drops below %d%%", reminderBelow), Description: templateDescription, Mode: "queued",
		Triggers: []any{map[string]any{"trigger": "numeric_state", "entity_id": batteries, "below": reminderBelow}}, Conditions: []any{},
		Actions: []any{map[string]any{"action": "notify." + phone.Service, "data": map[string]any{"title": "Low battery",
			"message": "{{ trigger.to_state.name }} is at {{ trigger.to_state.state }}%. Time for a new battery."}}}}
	return m, true
}

// linkedPhone: the phone linked for confirmations when it can be reached
// now, else nil (no reminder is offered).
func linkedPhone(s *relaySession) *phoneTarget {
	if !phoneLinkedHere(s.paths) {
		return nil
	}
	setup, status := discoverPhone(s)
	if status != phoneReady || len(setup.targets) == 0 {
		return nil
	}
	return &setup.targets[0]
}

// templatePlan builds (never stores) the automation create plan of a match.
func templatePlan(paths runtimePaths, s *relaySession, env *verbEnvelope, m templateMatch) (*tierEngine, storedPlan, error) {
	draft, err := json.Marshal(m.draft)
	if err != nil {
		return nil, storedPlan{}, err
	}
	req := configRequest{Op: "write", Domain: "automation", ConfigID: strconv.FormatInt(verbNow().UnixMilli(), 10), Create: true, Minted: true, Draft: draft}
	if err := req.validate(); err != nil {
		return nil, storedPlan{}, err
	}
	e, plan, _, err := computeConfigPlan(paths, s, env, req)
	return e, plan, err
}

// templateOffered: the match's plan would be preview tier (computed, not
// stored), so the page may offer it.
func templateOffered(paths runtimePaths, s *relaySession) func(templateMatch) bool {
	return func(m templateMatch) bool {
		e, _, err := templatePlan(paths, s, &verbEnvelope{Verb: "map", Server: s.server}, m)
		return err == nil && e.level == tierPreview
	}
}

func runPlanTemplate(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("plan template", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	positional, err := parseVerbArgs(fs, &opts, args, planTemplateUsage+"\n\nRead-only: fills the template with this home's entities and stores an automation create plan (preview tier only); \"ha-nova apply\" saves it.")
	if err == nil && (len(positional) != 1 || !slices.Contains(automationTemplates, positional[0])) {
		err = errors.New("Usage: " + planTemplateUsage)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	id := positional[0]
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		home, err := loadHome(s)
		if err != nil {
			return err
		}
		var phone *phoneTarget
		if id == templateBatteryReminder {
			phone = linkedPhone(s)
		}
		m, ok := indexHome(home, time.Now()).matchTemplate(id, phone)
		if !ok {
			return refusedWith("TEMPLATE_NO_MATCH", "this home has nothing the "+id+" template fits now", "Don't offer it; nothing to save.")
		}
		e, plan, err := templatePlan(paths, s, env, m)
		if err != nil {
			return err
		}
		if e.level != tierPreview { // only preview plans are offered (spec §7.5); this one is never stored
			env.Plan = nil
			return refusedWith("TEMPLATE_NOT_OFFERED", "this suggestion would need the user's own confirmation code, so it is not offered", "Don't offer it; nothing to save.")
		}
		env.Result = m
		return savePlan(paths, plan, s.transport.Credential)
	})
}
