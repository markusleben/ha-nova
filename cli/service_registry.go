package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// serviceSpec is what /api/services says about one service: listed,
// takes a target, runs only with return_response. known is false when the
// list cannot be read.
type serviceSpec struct{ listed, target, responseOnly, known bool }

// serviceSpec reads Home Assistant's service registry once per run.
func (e *tierEngine) serviceSpec(service string) serviceSpec {
	if !e.servicesRead {
		e.servicesRead = true
		e.services = readServices(e.s)
	}
	if e.services == nil {
		return serviceSpec{}
	}
	spec := e.services[service] // zero: not listed
	spec.known = true
	return spec
}

// readServices maps "domain.service" to its spec; nil when unreadable.
func readServices(s *relaySession) map[string]serviceSpec {
	status, body, err := s.coreGET("/api/services")
	var domains []struct {
		Domain   string `json:"domain"`
		Services map[string]struct {
			Target   json.RawMessage `json:"target"`
			Response *struct {
				Optional bool `json:"optional"`
			} `json:"response"`
		} `json:"services"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &domains) != nil || domains == nil {
		return nil
	}
	out := map[string]serviceSpec{}
	for _, d := range domains {
		for name, spec := range d.Services {
			out[d.Domain+"."+name] = serviceSpec{listed: true, target: spec.Target != nil,
				responseOnly: spec.Response != nil && !spec.Response.Optional}
		}
	}
	return out
}

// refuseUnlisted refuses a service Home Assistant does not have, before any
// tier: an invented name must never reach a confirmation. An unreadable
// list proves nothing, so it refuses too (fail closed).
func (e *tierEngine) refuseUnlisted(env *verbEnvelope, req planCallRequest, spec serviceSpec) error {
	if !spec.known {
		f := refuse(env, tierReason{Code: reasonServiceListUnread, Via: req.Service})
		f.next = "Home Assistant's service list could not be read, so " + req.Service + " is unproven. Plan again in a moment, show its preview, and get confirmation again; if it keeps failing, run ha-nova doctor."
		return f
	}
	f := refuse(env, tierReason{Code: reasonServiceNotFound, Via: req.Service, Detail: "not listed in /api/services"})
	f.next = "Home Assistant doesn't list the service " + req.Service + "."
	if candidates := e.serviceCandidates(req); len(candidates) > 0 {
		f.next += " Real candidates: " + strings.Join(candidates, "; ") + "."
	} else {
		f.next += " Read the real services from /api/services first."
	}
	return f
}

// serviceCandidates names up to 3 real alternatives: for reset/restart
// wording, the press of the target device's buttons that name the same
// operation first (never a destructive one the name lacks: "reset" never
// offers a factory reset), then the domain's own services.
func (e *tierEngine) serviceCandidates(req planCallRequest) []string {
	var out []string
	domain, name := splitID(req.Service)
	if words := operationWords(name); len(words) > 0 {
		ids := append([]string{}, req.EntityIDs...)
		if req.Match != "" || req.Area != "" {
			resolved, _ := e.resolveTargets(req) // best effort: only their devices matter
			for _, t := range resolved {
				ids = append(ids, t.EntityID)
			}
		}
		devices := []string{req.Device}
		for _, id := range ids {
			devices = append(devices, e.reg[id].di)
		}
		for _, device := range devices {
			if device == "" {
				continue
			}
			for _, button := range e.deviceEntities(device, "button") {
				st, _ := e.state(button)
				if sameOperation(words, operationWords(button+" "+st.str("friendly_name"))) && !slices.Contains(out, "button.press on "+button) {
					out = append(out, "button.press on "+button)
				}
			}
		}
	}
	var siblings []string
	for id := range e.services {
		if strings.HasPrefix(id, domain+".") {
			siblings = append(siblings, id)
		}
	}
	sort.Strings(siblings)
	out = append(out, siblings...)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// sameOperation: the button shares an operation word with the service and
// carries no destructive word the service lacks.
func sameOperation(service, button []string) bool {
	shared := false
	for _, word := range button {
		if slices.Contains(destructiveButtonWords, word) && !slices.Contains(service, word) {
			return false
		}
		shared = shared || slices.Contains(service, word)
	}
	return shared
}

// operationWords: the destructive and disruptive button words in text
// ("smartplug_factory_reset" → factory, reset).
func operationWords(text string) []string {
	var out []string
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if slices.Contains(destructiveButtonWords, token) || slices.Contains(disruptiveButtonWords, token) {
			out = append(out, token)
		}
	}
	return out
}
