package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// The worker's half of an ask (docs/work/2026-11-03-relay-typed-gate-spec.md
// §3): /ask rates every request of an explicit ask and words it for the
// person, /phone asks the linked phone and judges the tap. The Relay keeps
// the ask, the grant, and the page; the worker only prepares texts and
// judges taps, with the CLI's own plans and phone code.

const (
	gateAskMax       = 20
	gatePhoneMaxWait = 3 * time.Minute // the ask's lifetime
	gatePhoneTag     = "ha-nova-gate"
)

type gateAskRequest struct {
	Endpoint string `json:"endpoint"`
	Body     []byte `json:"body_b64"`
}

type gateAskItem struct {
	Verdict   gateDecision `json:"verdict"`
	Why       string       `json:"why,omitempty"`
	Text      string       `json:"text,omitempty"`
	Detail    string       `json:"detail,omitempty"` // the reasons the text doesn't say, uncut, for the page
	EntityIDs []string     `json:"entity_ids,omitempty"`
}

// gateAsk: the strictest item's verdict; for typed, the phone's title, body,
// and button, and each typed item's text for the page.
type gateAsk struct {
	Verdict gateDecision  `json:"verdict"`
	Lang    string        `json:"lang,omitempty"`
	Title   string        `json:"title,omitempty"`
	Body    string        `json:"body,omitempty"`
	Button  string        `json:"button,omitempty"`
	Items   []gateAskItem `json:"items"`
}

// gatePrepareAsk rates each request on its own fresh session (Contract),
// then words the typed ones on one session. Only typed items are worded and
// listed: a grant covers only them, and the rest run without one. Nothing
// is worded unless one is typed and none is refused. err: a check or a
// wording failed (unavailable).
func gatePrepareAsk(paths runtimePaths, device string, requests []gateAskRequest) (gateAsk, error) {
	ask := gateAsk{Verdict: gatePass, Items: make([]gateAskItem, len(requests))}
	rank := map[gateDecision]int{gatePass: 0, gateTyped: 1, gateRefuse: 2}
	var typed []int
	for i, r := range requests {
		check, err := gateCheckRequest(paths, r.Endpoint, r.Body)
		if err != nil {
			return gateAsk{}, err
		}
		ask.Items[i] = gateAskItem{Verdict: check.Verdict, Why: check.Why}
		if rank[check.Verdict] > rank[ask.Verdict] {
			ask.Verdict = check.Verdict
		}
		if check.Verdict == gateTyped {
			typed = append(typed, i)
		}
	}
	if ask.Verdict != gateTyped {
		return ask, nil
	}
	s, err := newRelaySessionWithin(paths, verbOptions{}, time.Duration(len(requests))*gateCheckBudget)
	if err != nil {
		return gateAsk{}, err
	}
	defer s.close()
	words := map[int]gateWords{}
	wordAll := func() (mixed bool, err error) {
		for _, i := range typed {
			if words[i], err = gateAskWords(s, requests[i].Endpoint, requests[i].Body, ask.Items[i].Why); err != nil {
				return false, fmt.Errorf("item %d can't be worded: %w", i+1, err)
			}
			mixed = mixed || words[i].lang != words[typed[0]].lang
		}
		return mixed, nil
	}
	mixed, err := wordAll()
	if err == nil && mixed { // never mixed (confirmation localization, Design 9): all of it in English
		s.lang, s.words = "en", nil
		_, err = wordAll()
	}
	if err != nil {
		return gateAsk{}, err
	}
	lang := words[typed[0]].lang
	ask.Lang = lang
	var why []string
	for _, i := range typed {
		w := words[i]
		ask.Items[i].Text, ask.Items[i].Detail, ask.Items[i].EntityIDs = upperFirst(w.goal.text(lang))+gateIDs(w.ids), w.goal.why, w.ids
		if len(requests) > 1 {
			why = append(why, confirmT(lang, "raw.step_why", "number", i+1, "change", ask.Items[i].Text))
		} else if ids := gateIDs(w.ids); ids != "" {
			why = append(why, strings.TrimSpace(ids))
		}
	}
	for _, i := range typed {
		if detail := words[i].goal.why; detail != "" && !slices.Contains(why, detail) {
			why = append(why, detail)
		}
	}
	goal := words[typed[0]].goal
	if len(requests) > 1 {
		goal = actionGoal{raw: "confirm", names: []string{confirmT(lang, "raw.steps", "count", len(requests))}}
	}
	body := confirmT(lang, "phone.requested_by", "host", sayText(device, false))
	if text := strings.Join(why, ". "); text != "" {
		if len([]rune(text)) > phoneWhyLimit {
			text = phoneCut(text, phoneWhyLimit-25) + " " + confirmT(lang, "phone.more_on_computer")
		}
		body = text + ". " + body
	}
	ask.Title, ask.Body, ask.Button = goal.title(lang, ""), body, goal.button(lang)
	return ask, nil
}

// gateWords: one request as its plan words it, in lang, with the entity ids
// it acts on.
type gateWords struct {
	goal actionGoal
	lang string
	ids  []string
}

// gateAskWords words one request as its plan does (sayAsk: the goal and the
// reasons its title doesn't say), read as the gate reads it (gateRequests:
// a collect_events envelope as its message). A raw shape no raw plan holds
// (a dashboard delete) is worded by its line and the gate's own reason
// (why); any other planner failure is a failure.
func gateAskWords(s *relaySession, endpoint string, body []byte, why string) (gateWords, error) {
	payload, ok := decodeObject(body)
	if !ok || payload == nil {
		return gateWords{}, errors.New("not one JSON object")
	}
	switch requests := gateRequests(endpoint, payload); {
	case len(requests) != 1:
		return gateWords{}, errors.New("an envelope with a type of its own")
	case endpoint == "ws" && payload["message"] != nil: // a collect_events envelope runs its message: word that, as sent
		var envelope struct {
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			return gateWords{}, errors.New("the envelope's message can't be read")
		}
		return gateAskWords(s, endpoint, envelope.Message, why)
	default:
		payload = requests[0] // a core method as the Relay sends it
	}
	env := verbEnvelope{SchemaVersion: 1, Verb: "gate"}
	path, _ := payload["path"].(string)
	method, _ := payload["method"].(string)
	switch {
	case endpoint == "core" && method == http.MethodPost && servicePathPattern.MatchString(path):
		e, plan, refusedShape, _, err := gatePlan(func() (*relaySession, error) { return s, nil }, &env, payload, body, "")
		if refusedShape {
			err = errors.New("a service call the CLI never sends")
		}
		if err != nil {
			return gateWords{}, err
		}
		ids := plan.Action.EntityIDs
		if domain, name := splitID(plan.Action.Service); domain == "script" && !scriptEntityServices[name] {
			ids = []string{plan.Action.Service} // the script it runs
		}
		return gateWords{sayAsk(e, &env, plan, ""), textLang(plan), gateValidIDs(ids)}, nil
	case endpoint == "core" && method != http.MethodGet && configPathPattern.MatchString(path):
		domain, id, _ := strings.Cut(strings.TrimPrefix(path, "/api/config/"), "/config/")
		entity, _, err := registryEntityFor(s, domain, id)
		if err != nil {
			return gateWords{}, err
		}
		e, plan, _, _, err := gatePlan(func() (*relaySession, error) { return s, nil }, &env, payload, body, entity)
		if err == nil && method == http.MethodDelete && (plan.ConfigAction == nil || plan.ConfigAction.ConfigID != id || plan.ConfigAction.Domain != domain) {
			err = errors.New("the delete resolved to another config")
		}
		if err != nil {
			return gateWords{}, err
		}
		return gateWords{sayAsk(e, &env, plan, ""), textLang(plan), gateValidIDs([]string{entity})}, nil
	}
	ids := gateEntityIDs(payload, nil, 0)
	e, plan, err := computeRawPlan(s, &env, []rawRequest{{Endpoint: endpoint, Payload: body}})
	if err == nil {
		return gateWords{sayAsk(e, &env, plan, ""), textLang(plan), ids}, nil
	}
	var failure *verbFailure
	say, known := rawWhys[why]
	if !errors.As(err, &failure) || failure.exit != verbExitRefused || !known { // only a shape no raw plan holds; a failed read stays a failure
		return gateWords{}, err
	}
	lang := s.language()
	verb, object := rawItemLine(newRawNamer(s, nil), lang, endpoint, payload)
	return gateWords{actionGoal{raw: verb, names: rawNames(lang, object, nil), why: say(lang)}, lang, ids}, nil
}

// gateEntityIDs: every entity id a raw request names, in order, once.
func gateEntityIDs(v any, ids []string, depth int) []string {
	if depth > 4 {
		return ids
	}
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if k == "entity_id" {
				ids = gateAppendIDs(ids, v[k])
				continue
			}
			ids = gateEntityIDs(v[k], ids, depth+1)
		}
	case []any:
		for _, item := range v {
			ids = gateEntityIDs(item, ids, depth+1)
		}
	}
	return ids
}

func gateAppendIDs(ids []string, v any) []string {
	values, isList := v.([]any)
	if !isList {
		values = []any{v}
	}
	for _, value := range values {
		if id, ok := value.(string); ok {
			ids = append(ids, id)
		}
	}
	return gateValidIDs(ids)
}

// gateValidIDs: the well-formed entity ids, once each.
func gateValidIDs(ids []string) []string {
	var valid []string
	for _, id := range ids {
		if entityIDPattern.MatchString(id) && !slices.Contains(valid, id) {
			valid = append(valid, id)
		}
	}
	return valid
}

// gateIDs: " (lock.front_door, cover.garage)", whole (only [a-z0-9_.]), so
// a renamed entity can't pass for another (spec §3).
func gateIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return " (" + strings.Join(ids, ", ") + ")"
}

type gatePhoneRequest struct {
	Lang   string    `json:"lang"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Button string    `json:"button"`
	Link   phoneLink `json:"link"`
	WaitMS int64     `json:"wait_ms"`
}

// gatePhoneAnswer: approved (only from the linked user's own app), declined,
// rejected (one of the ask's ids came from someone else), none (no answer in
// time), or unavailable (the phone can't be asked; nothing was sent, or the
// send failed). why says why it is unavailable.
type gatePhoneAnswer struct {
	Answer string `json:"answer"`
	UserID string `json:"user_id,omitempty"`
	Why    string `json:"why,omitempty"`
}

func (in gatePhoneRequest) valid() bool {
	_, known := confirmCatalogs[in.Lang]
	wait := time.Duration(in.WaitMS) * time.Millisecond
	return known && in.Title != "" && in.Body != "" && in.Button != "" && phoneServicePattern.MatchString(in.Link.Service) &&
		in.Link.DeviceID != "" && in.Link.UserID != "" && wait >= time.Second && wait <= gatePhoneMaxWait
}

// gatePhoneSetup reads the home with the Relay's own link for this device
// (lookupPhones, home.status): the confirmer must still be an active,
// non-system administrator who is not the Relay's user, and the phone must
// still be reachable. Tests replace it.
var gatePhoneSetup = func(s *relaySession, link phoneLink) (phoneSetup, phoneStatus) {
	var users []haUser
	if err := s.phoneRead("ws", map[string]any{"type": "config/auth/list"}, &users); err != nil {
		return phoneSetup{why: whyRead}, phoneMissing
	}
	owner := ""
	for _, u := range users {
		if u.IsOwner && u.IsActive && !u.SystemGenerated {
			owner = u.ID
		}
	}
	links := map[string]phoneEntry{owner: {link: &link}}
	home, err := lookupPhones(s, links)
	return home.status(links, err)
}

// gatePhone asks the linked phone and waits for its answer, at most the
// request's wait or until the Relay hangs up (ctx). The notification is
// withdrawn whatever the answer: the Relay says how it ended.
func gatePhone(ctx context.Context, paths runtimePaths, in gatePhoneRequest) (gatePhoneAnswer, error) {
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		return gatePhoneAnswer{}, err
	}
	defer s.close()
	setup, status := gatePhoneSetup(s, in.Link)
	if status != phoneReady {
		return gatePhoneAnswer{Answer: "unavailable", Why: gatePhoneWhy(setup.why)}, nil
	}
	ask := phoneAsk{lang: in.Lang, title: phoneCut(in.Title, phoneTitleLimit), body: in.Body, button: in.Button, urgent: true}
	answer, reply := askPhone(ctx, s, setup, gatePhoneTag, ask, time.Duration(in.WaitMS)*time.Millisecond, nil)
	defer reply.withdraw(ask.title, ask.body)
	switch answer {
	case phoneConfirmed:
		return gatePhoneStill(paths, in.Link, setup), nil
	case phoneDeclined:
		return gatePhoneAnswer{Answer: "declined"}, nil
	case phoneRejected:
		return gatePhoneAnswer{Answer: "rejected"}, nil
	case phoneNoAnswer:
		return gatePhoneAnswer{Answer: "none"}, nil
	}
	return gatePhoneAnswer{Answer: "unavailable", Why: "send"}, nil
}

// gatePhoneStill: a tap counts only if, read again after it on a fresh
// session, the same administrator may still confirm on the same phone; a
// demotion during the wait, or a failed read, is no approval.
func gatePhoneStill(paths runtimePaths, link phoneLink, setup phoneSetup) gatePhoneAnswer {
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		return gatePhoneAnswer{Answer: "unavailable", Why: "read"}
	}
	defer s.close()
	again, status := gatePhoneSetup(s, link)
	if status != phoneReady || again.confirmer != setup.confirmer || again.targets[0].DeviceID != setup.targets[0].DeviceID {
		return gatePhoneAnswer{Answer: "unavailable", Why: cmp.Or(gatePhoneWhy(again.why), "changed")}
	}
	return gatePhoneAnswer{Answer: "approved", UserID: setup.confirmer}
}

func gatePhoneWhy(why phoneWhy) string {
	return map[phoneWhy]string{whyRead: "read", whyOwnerToken: "owner_token", whyRelayUser: "relay_user", whyDemoted: "not_admin",
		whyRemoved: "removed", whyShared: "shared_name", whyUnreachable: "unreachable"}[why]
}

// gateAskHandler answers /ask and /phone; the caller checked the bearer.
// /ask rates up to 20 requests and words them, a check budget for each, twice.
func gateAskHandler(paths runtimePaths, w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, gateBodyLimit))
	dec.DisallowUnknownFields()
	var out any
	var err error
	if r.URL.Path == "/ask" {
		var in struct {
			Device   string           `json:"device"`
			Requests []gateAskRequest `json:"requests"`
		}
		if dec.Decode(&in) != nil || in.Device == "" || len(in.Requests) == 0 || len(in.Requests) > gateAskMax ||
			slices.ContainsFunc(in.Requests, func(r gateAskRequest) bool { return r.Endpoint == "" }) {
			http.Error(w, "the request must be one JSON object with device and 1-20 requests, each with endpoint", http.StatusBadRequest)
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(2*len(in.Requests)+2) * gateCheckBudget))
		out, err = gatePrepareAsk(paths, in.Device, in.Requests)
	} else {
		var in gatePhoneRequest
		if dec.Decode(&in) != nil || !in.valid() {
			http.Error(w, "the request must be one JSON object with lang, title, body, button, link, and wait_ms up to 180000", http.StatusBadRequest)
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Duration(in.WaitMS)*time.Millisecond + time.Minute))
		out, err = gatePhone(r.Context(), paths, in)
	}
	if err != nil { // nothing was asked or sent: the Relay answers 503
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
