package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// The service calls a plan's cascade makes, for call_service listeners
// (MAR-127): Home Assistant fires call_service for every call through its
// service registry, including the calls an entity platform or a scene makes
// for the plan. A listener's event_data domain/service filter decides only
// against an over-approximation of those calls; anything the engine can't
// bound makes the set unknown, and then every listener counts (fail closed).

// callSet holds "domain.service" and "domain.*" entries.
type callSet struct {
	unknown bool
	calls   map[string]bool
}

// callingPlatforms: built-in platforms whose entities call services when
// actuated, from Home Assistant 2026.11's own code (services.async_call,
// Script(, device actions); re-checked in docs/work/upstream-drift-log.md.
// "domain" scopes one entity domain of a platform; "" every domain. group,
// switch_as_x, and scenes are modeled instead (their members are visited);
// a generic thermostat or hygrostat calls what helperCalls bounds.
var callingPlatforms = map[string]string{
	"template": "", "universal": "", "flux": "", "wake_on_lan": "",
	"alert": "", "pilight": "", "lifx": "light", "panasonic_viera": "media_player", "cast": "media_player", "demo": "vacuum", "marketplace": "update",
}

// forwards: built-in services whose handlers call other services the engine
// doesn't follow (tts.speak plays on a media player, a notify platform may
// speak or notify a group, conversation runs intents, a mobile_app device
// action notifies); "" every service of the domain. Calling one makes the
// set unknown.
var forwards = map[string]string{
	"tts": "", "notify": "", "conversation": "", "python_script": "", "color_extractor": "", "media_extractor": "", "mobile_app": "", "camera": "play_stream",
}

// reactors: built-in integrations that call services on their own when other
// entities change, outside automations. What they call, and what that sets
// off in turn, isn't bounded (Markus, 2026-11-09, option B; made unknown
// after Astra's review), so a home with one loaded makes the set unknown.
// YAML trigger-based template entities with actions, custom integrations,
// and device firmware (an ESPHome homeassistant.action) are outside the
// model (safety.md). A generic thermostat or hygrostat is bounded by its
// options instead (helperCalls).
var reactors = map[string]bool{"alert": true, "flux": true, "device_sun_light_trigger": true}

// called records a service the cascade calls by name.
func (e *tierEngine) called(name string) {
	domain, service := splitID(strings.ToLower(name))
	if isTemplated(name) || domain == "" || service == "" {
		e.calls.unknown = true
		return
	}
	e.calls.add(domain + "." + service)
}

// calledOn records a call that reaches entity id: whatever service its
// platform turns it into (a toggle into turn_on/turn_off, a scene's state
// into attribute services, homeassistant.* into the entity's domain) is a
// service of that domain; a platform that calls services itself, or one the
// engine can't name, makes the set unknown. A switch_as_x entity answers the
// switches it actuates (on its device), for the caller to visit.
func (e *tierEngine) calledOn(id string) []string {
	domain, _ := splitID(id)
	e.calls.add(domain + ".*")
	entry, registered := e.reg[id]
	if !registered || entry.pl == "" {
		e.calls.unknown = true
		return nil
	}
	if entry.pl == "switch_as_x" {
		var backing []string
		for other, row := range e.reg {
			if entry.di != "" && row.di == entry.di && row.pl != "switch_as_x" && strings.HasPrefix(other, "switch.") {
				backing = append(backing, other)
			}
		}
		if len(backing) == 0 {
			e.calls.unknown = true
		}
		slices.Sort(backing)
		return backing
	}
	if scope, calls := callingPlatforms[entry.pl]; calls && (scope == "" || scope == domain) || !e.builtIn(entry.pl) {
		e.calls.unknown = true
	}
	return nil
}

func (c *callSet) add(entry string) {
	if c.calls == nil {
		c.calls = map[string]bool{}
	}
	c.calls[entry] = true
	domain, service := splitID(entry)
	if forwarded, forwards := forwards[domain]; forwards && (forwarded == "" || forwarded == service) {
		c.unknown = true
	}
}

// mayFire: an event trigger fires when every event_data key matches; only a
// literal domain or service that no call carries rules a listener out.
func (c *callSet) mayFire(filter map[string]any) bool {
	if c.unknown {
		return true
	}
	literal := func(key string) (string, bool) {
		s, isString := filter[key].(string)
		return strings.ToLower(s), isString && !isTemplated(s)
	}
	wantDomain, domainKnown := literal("domain")
	wantService, serviceKnown := literal("service")
	for entry := range c.calls {
		domain, service := splitID(entry)
		if (!domainKnown || domain == wantDomain) && (!serviceKnown || service == "*" || service == wantService) {
			return true
		}
	}
	return false
}

// builtIn: the integration ships with Home Assistant (manifest/list); an
// unreadable list or a missing entry is not built in.
func (e *tierEngine) builtIn(platform string) bool {
	if e.manifests == nil {
		var list []struct {
			Domain  string `json:"domain"`
			BuiltIn bool   `json:"is_built_in"`
		}
		e.manifests = map[string]bool{}
		if err := e.s.wsDecode(map[string]any{"type": "manifest/list"}, &list, "the integrations"); err == nil {
			for _, m := range list {
				e.manifests[m.Domain] = m.BuiltIn
			}
		}
	}
	return e.manifests[platform]
}

// reactorCalls makes the set unknown when a reactor is loaded in this home
// (/api/config components: "flux" or "flux.switch"), or the list is unread;
// a loaded thermostat or hygrostat helper adds what it calls.
func (e *tierEngine) reactorCalls() {
	var config struct {
		Components []string `json:"components"`
	}
	status, body, err := e.s.coreGET("/api/config")
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &config) != nil {
		e.calls.unknown = true
		return
	}
	helpers := false
	for _, component := range config.Components {
		name, _, _ := strings.Cut(component, ".")
		e.calls.unknown = e.calls.unknown || reactors[name]
		helpers = helpers || helperOptionFields[name] != ""
	}
	if helpers {
		e.helperCalls()
	}
}

// callServiceListeners adds every call_service listener that may fire for
// the cascade's calls, until none is left: a listener's own run calls more.
// A run that isn't fully read can't bound its calls.
func (e *tierEngine) callServiceListeners() {
	table, _ := e.eventCounts("call_service", "")
	if table == nil {
		return
	}
	e.reactorCalls()
	hits := table.hits["call_service"]
	if e.callAdded == nil {
		e.callAdded = map[int]bool{}
	}
	for grew := true; grew; {
		grew = false
		for i, hit := range hits {
			if !e.callAdded[i] && (!e.complete || e.calls.mayFire(hit.filter)) {
				e.callAdded[i], grew = true, true
				e.consumer(hit.automation, "event:call_service", "", 1, nil)
			}
		}
	}
}
