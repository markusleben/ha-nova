package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Log facts (docs/work/2026-11-12-log-facts-spec.md): the one log read HA
// NOVA answers, WS {"type":"system_log/list"} with no other key, is answered
// with counts and times per integration and level, never with the log. Every
// string in the answer is a domain of this home's integrations, a fixed level
// word, or a time HA NOVA formatted: no message, exception, logger name, or
// path from the log ever passes.

const logFactsBody = `{"type":"system_log/list"}`

var (
	logFactsLevels  = []string{"DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"}
	logFactsDomain  = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	logFactsCode    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	logFactsLatest  = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	errLogFactsForm = errors.New("Home Assistant's log answer has an unknown shape; nothing of it is returned")
)

type logFact struct {
	Domain        string `json:"domain"`
	Level         string `json:"level"`
	Count         int64  `json:"count"`
	FirstRetained string `json:"first_retained"`
	Last          string `json:"last"`
}

type logFacts struct {
	LogFacts          []logFact `json:"log_facts"`
	UnattributedCount int64     `json:"unattributed_count"`
}

// logFactsRequest: body is exactly {"type":"system_log/list"} on ws.
func logFactsRequest(endpoint string, body []byte) bool {
	payload, ok := decodeObject(body)
	return endpoint == "ws" && ok && json.Valid(body) && len(payload) == 1 && payload["type"] == "system_log/list"
}

// readLogFacts reads the log on s and answers its facts. A 0.31 App's gate
// already answers facts; the chat bridge and an App without the gate get the
// list and project it here. Every failure is worded here: Home Assistant's
// own error text could quote the log, so none of it passes.
func readLogFacts(s *relaySession) (json.RawMessage, error) {
	data, err := s.send("ws", []byte(logFactsBody))
	if err != nil {
		return nil, logFactsFailure(err)
	}
	var domains map[string]bool
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		if domains, err = logFactsDomains(s); err != nil {
			return nil, logFactsFailure(err)
		}
	}
	return projectLogFacts(data, domains)
}

// logFactsFailure: an answer of the Relay or Home Assistant (its message
// could quote the log) is worded with its HTTP status and fixed error code
// only; HA NOVA's own words (unreachable, pairing, timeouts) pass.
func logFactsFailure(err error) error {
	var call *relayCallError
	if !errors.As(err, &call) {
		return err
	}
	code := "ERROR"
	if logFactsCode.MatchString(call.code) {
		code = call.code
	}
	message := "HA NOVA could not read the log facts; nothing was changed"
	if code == "GATE_REFUSED" { // the 0.30 gate refuses every log read
		message += ". This NOVA App is older than 0.31 and refuses them: update it, or ask the user to read Settings > System > Logs"
	}
	return &relayCallError{endpoint: "ws", status: call.status, code: code, message: message}
}

// logFactsDomains: this home's integrations (manifest/list, a read the AI
// already has). A failed read is an error, never an empty list.
func logFactsDomains(s *relaySession) (map[string]bool, error) {
	var manifests []struct {
		Domain string `json:"domain"`
	}
	if err := s.wsDecode(map[string]any{"type": "manifest/list"}, &manifests, "the integration list"); err != nil {
		return nil, err
	}
	if manifests == nil {
		return nil, errors.New("the integration list is not a list")
	}
	domains := map[string]bool{}
	for _, m := range manifests {
		if logFactsDomain.MatchString(m.Domain) {
			domains[m.Domain] = true
		}
	}
	return domains, nil
}

// projectLogFacts turns system_log/list's answer (a list, with domains this
// home's integrations) into facts. Facts a gate already built (an object)
// are checked field by field and rebuilt the same way, so projecting twice
// changes nothing; domains is not needed for them. Anything else is an
// error, and nothing of it is returned.
func projectLogFacts(data json.RawMessage, domains map[string]bool) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, errLogFactsForm
	}
	type group struct {
		count       int64
		first, last time.Time
	}
	groups := map[[2]string]*group{}
	var out logFacts
	add := func(domain, level string, count int64, first, last time.Time) {
		key := [2]string{domain, level}
		g := groups[key]
		if g == nil {
			g = &group{first: first, last: last}
			groups[key] = g
		}
		g.count = min(g.count+count, logFactsMaxCount)
		g.first = minTime(g.first, first)
		g.last = maxTime(g.last, last)
	}
	switch v := value.(type) {
	case []any: // the log itself
		for _, item := range v {
			entry, _ := item.(map[string]any)
			domain := logFactsDomainOf(entry["name"], domains)
			level, _ := entry["level"].(string)
			count, countOK := logFactsCount(entry["count"], 1)
			first, firstOK := logFactsUnix(entry["first_occurred"])
			last, lastOK := logFactsUnix(entry["timestamp"])
			if domain == "" || !slices.Contains(logFactsLevels, level) || !countOK || !firstOK || !lastOK {
				out.UnattributedCount = min(out.UnattributedCount+max(count, 1), logFactsMaxCount)
				continue
			}
			add(domain, level, count, first, last)
		}
	case map[string]any: // facts a gate built
		facts, _ := v["log_facts"].([]any)
		unattributed, unattributedOK := logFactsCount(v["unattributed_count"], 0)
		if len(v) != 2 || facts == nil || !unattributedOK {
			return nil, errLogFactsForm
		}
		out.UnattributedCount = unattributed
		for _, item := range facts {
			fact, _ := item.(map[string]any)
			domain, _ := fact["domain"].(string)
			level, _ := fact["level"].(string)
			count, countOK := logFactsCount(fact["count"], 1)
			first, firstOK := logFactsTime(fact["first_retained"])
			last, lastOK := logFactsTime(fact["last"])
			if len(fact) != 5 || !logFactsDomain.MatchString(domain) || !slices.Contains(logFactsLevels, level) || !countOK || !firstOK || !lastOK {
				return nil, errLogFactsForm
			}
			add(domain, level, count, first, last)
		}
	default:
		return nil, errLogFactsForm
	}
	out.LogFacts = []logFact{}
	for key, g := range groups {
		out.LogFacts = append(out.LogFacts, logFact{Domain: key[0], Level: key[1], Count: g.count,
			FirstRetained: g.first.Format(time.RFC3339), Last: g.last.Format(time.RFC3339)})
	}
	slices.SortFunc(out.LogFacts, func(a, b logFact) int {
		return cmp.Or(strings.Compare(b.Last, a.Last), strings.Compare(a.Domain, b.Domain), strings.Compare(a.Level, b.Level))
	})
	return json.Marshal(out)
}

const logFactsMaxCount = 1e12

// logFactsDomainOf: the integration a logger name belongs to, if it is one of
// domains: homeassistant.components.<d> and custom_components.<d> (the first
// segment only: an automation's logger carries its own suffix); any other
// homeassistant.* logger is homeassistant.
func logFactsDomainOf(name any, domains map[string]bool) string {
	logger, _ := name.(string)
	var domain string
	switch {
	case strings.HasPrefix(logger, "homeassistant.components."):
		domain, _, _ = strings.Cut(strings.TrimPrefix(logger, "homeassistant.components."), ".")
	case strings.HasPrefix(logger, "custom_components."):
		domain, _, _ = strings.Cut(strings.TrimPrefix(logger, "custom_components."), ".")
	case logger == "homeassistant" || strings.HasPrefix(logger, "homeassistant."):
		domain = "homeassistant"
	}
	if !domains[domain] || !logFactsDomain.MatchString(domain) {
		return ""
	}
	return domain
}

// logFactsCount: a whole number from least to logFactsMaxCount.
func logFactsCount(v any, least float64) (int64, bool) {
	n, ok := v.(float64)
	if !ok || n != math.Trunc(n) || n < least || n > logFactsMaxCount {
		return 0, false
	}
	return int64(n), true
}

// logFactsUnix: Home Assistant's time, seconds since 1970, to the second.
func logFactsUnix(v any) (time.Time, bool) {
	seconds, ok := v.(float64)
	if !ok || math.IsNaN(seconds) || seconds < 1 || seconds >= float64(logFactsLatest.Unix()) {
		return time.Time{}, false
	}
	return time.Unix(int64(seconds), 0).UTC(), true
}

// logFactsTime: a time HA NOVA wrote (RFC 3339, UTC, whole seconds).
func logFactsTime(v any) (time.Time, bool) {
	text, _ := v.(string)
	t, err := time.Parse(time.RFC3339, text)
	if err != nil || t.UTC().Format(time.RFC3339) != text || t.Unix() < 1 || !t.Before(logFactsLatest) {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// runRelayLogFacts answers `ha-nova relay ws` with the log read's facts,
// printed as the Relay's envelope like any ws read.
func runRelayLogFacts(paths runtimePaths, target verbOptions, jq *jqProgram, outFile string) int {
	s, err := newRelaySession(paths, target)
	var facts json.RawMessage
	if err == nil {
		if refusal := mcpDemoRefusal(activeServerIsDemo(), "ws", []byte(logFactsBody)); refusal != nil { // as ha_read: the demo's log is other visitors'
			s.close()
			printErr("%s", refusal)
			return rawRefusedExit
		}
		facts, err = readLogFacts(s)
		s.close()
	}
	var call *relayCallError
	if errors.As(err, &call) && call.code == "GATE_REFUSED" {
		printErr("%s", err)
		return rawRefusedExit
	}
	if err != nil {
		printErr("%s", relayReadFailed(err.Error()))
		return 1
	}
	out := append(append([]byte(`{"ok":true,"data":`), facts...), '}', '\n')
	if jq != nil {
		res, err := applyCompiledJQFilter(jq, out, false)
		if err != nil {
			printErr("%s", relayReadFailed("jq filtering failed: "+err.Error()))
			return 1
		}
		out = []byte(res.output)
	}
	if outFile != "" {
		err = writeRelayTextOutput(outFile, out)
	} else {
		_, err = os.Stdout.Write(out)
	}
	if err != nil {
		printErr("%s", relayReadFailed("writing the output failed: "+err.Error()))
		return 1
	}
	return 0
}
