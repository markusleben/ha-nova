package main

import (
	"encoding/json"
	"slices"
)

// The YAML thermostats and hygrostats (MAR-245,
// docs/work/2026-11-20-yaml-helper-read-spec.md): bounded from the YAML
// Home Assistant provably runs (yamlHelpersEndpoint), else unknown with the
// cause kept for the plan (reasonHelperCalls).

// yamlHelperCalls bounds the YAML helpers (integration → its entities
// without a config entry): exactly as many YAML entries as running
// entities, each switched entity a plain switch or fan, and a YAML template
// heater only through its own plain turn_on/turn_off. False: the set is
// unknown, and helperCause names the first helper and why, or the read had
// no answer (readFailed).
func (e *tierEngine) yamlHelperCalls(yaml map[string][]string, rowOf map[string]fullRegistryRow) bool {
	var named []string
	for _, ids := range yaml {
		named = append(named, ids...)
	}
	fail := func(detail string) bool {
		e.helperUnbounded(slices.Min(named), detail)
		return false
	}
	for integration := range yaml {
		if !e.builtIn(integration) {
			return fail("not_built_in")
		}
	}
	var proj yamlProjection
	data, err := e.s.read(yamlHelpersEndpoint, map[string]any{})
	switch {
	case err != nil: // no answer (the budget, a timeout, the link): a failed read, as any read (#827)
		e.calls.unknown = true
		e.readFailed(slices.Min(named), "", "yaml helpers")
		return false
	case json.Unmarshal(data, &proj) != nil:
		return fail(yamlUnread)
	case proj.Failure != "":
		return fail(proj.Failure)
	}
	integrations := []string{"generic_hygrostat", "generic_thermostat"}
	for _, integration := range integrations {
		if len(proj.Helpers[integration]) != len(yaml[integration]) {
			return fail("yaml_mismatch")
		}
	}
	for _, integration := range integrations {
		for _, heater := range proj.Helpers[integration] {
			if !e.plainTarget(heater) {
				return fail("heater_not_plain")
			}
			e.calls.add("homeassistant.turn_on")
			e.calls.add("homeassistant.turn_off")
			if row := rowOf[heater]; row.Platform == "template" && row.ConfigEntryID == "" {
				if !e.templateHeater(heater, string(row.UniqueID), proj) {
					return fail("heater_actions")
				}
				continue
			}
			if e.calledOn(heater); e.calls.unknown {
				return fail("heater_not_plain")
			}
		}
	}
	return true
}

// plainTarget: a plain, quiet switch or fan (plainSwitched) that runs no
// user-written command and shares its device with nothing that opens.
func (e *tierEngine) plainTarget(id string) bool {
	return e.plainSwitched(id) && !userCommandPlatforms[e.reg[id].pl] && e.deviceAccessEntity(e.reg[id].di, true) == ""
}

// templateHeater adds what a YAML template switch calls when it is switched:
// only on this helper edge, so a direct call of a template entity keeps
// calledOn's rule. Its one entry must be plain turn_on/turn_off steps on
// plain targets of the service's own domain (or homeassistant).
func (e *tierEngine) templateHeater(heater, uid string, proj yamlProjection) bool {
	sw, found := proj.Switches[uid]
	if uid == "" || !found || sw.Count != 1 || !sw.Plain || proj.Unreadable || !e.builtIn("template") {
		return false
	}
	domain, _ := splitID(heater)
	e.calls.add(domain + ".*")
	for _, step := range sw.Steps {
		service, _ := splitID(step.Service)
		for _, target := range step.Targets {
			if own, _ := splitID(target); !e.plainTarget(target) || service != "homeassistant" && service != own {
				return false
			}
			e.calledOn(target)
		}
		e.called(step.Service)
	}
	return !e.calls.unknown
}

// yamlHelpersRun: a thermostat or hygrostat in the entity sources has no
// config entry, so the check will read the YAML helpers (helperCalls). That
// read is a chain of file reads; warmCensus sends it early, so the budget
// doesn't cut it where the check reaches it late.
func (s *relaySession) yamlHelpersRun() bool {
	var sources map[string]struct {
		Domain string `json:"domain"`
	}
	var rows []fullRegistryRow
	if s.wsDecode(map[string]any{"type": "entity/source"}, &sources, "the entity sources") != nil ||
		s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &rows, "the entity registry") != nil {
		return false
	}
	entryOf := map[string]string{}
	for _, row := range rows {
		entryOf[row.EntityID] = row.ConfigEntryID
	}
	for id, source := range sources {
		if helperOptionFields[source.Domain] != "" && entryOf[id] == "" {
			return true
		}
	}
	return false
}
