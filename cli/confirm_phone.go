package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Channel B (docs/work/2026-10-17-human-confirmation-channels-spec.md §7):
// a Companion-app notification on the linked phone (confirm_phone_link.go)
// with Confirm and Cancel. The action ids are random nonces that live only
// in this process; the tap counts only when Home Assistant reports it from
// the linked user's own app (context.user_id = that user, origin REMOTE),
// never from the Relay's own user. Where the Relay acts as the owner (a
// standalone Relay on the owner's token) B is unavailable.

type phoneAnswer int

const (
	phoneIgnored   phoneAnswer = iota // not ours: keep waiting
	phoneNoAnswer                     // timed out: nothing is sent
	phoneConfirmed                    // the linked user tapped Confirm
	phoneDeclined                     // the linked user tapped Cancel
	phoneRejected                     // one of our ids came from someone else: stop, nothing is sent
	phoneFailed                       // the notification could not be sent: nothing is sent, no fallback
)

const (
	cliPhoneTellSpan = 10 * time.Second
	phoneEventType   = "mobile_app_notification_action"
	phoneWindowSpan  = 10 * time.Second // the Relay's collect_events cap; two lanes overlap by half
)

var (
	phoneApplied      func(env verbEnvelope, exit int) // set by confirmByPhone: apply tells the phone how it ended, after its output
	phoneTellSpan     = cliPhoneTellSpan               // the result reaches a slow Relay before the process exits
	phoneSettle       = time.Second                    // a failing subscription returns at once; a working one has acked by then
	phoneNothingAfter = 30 * time.Second               // no answer yet: the terminal says where notifications turn on
	// The Relay calls; tests replace them.
	phoneDiscover = discoverPhone
	phoneNotify   = func(s *relaySession, service string, body map[string]any) error {
		result, err := s.postService("notify."+service, body, false)
		if err == nil && result.status/100 != 2 {
			err = fmt.Errorf("Home Assistant answered HTTP %d", result.status)
		}
		return err
	}
	phoneCollect = collectPhoneEvents
)

type phoneTarget struct {
	Service  string `json:"service"` // notify service: mobile_app_<slug>
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"` // the person's user it is listed under
	Person   string `json:"person"`
	Maker    string `json:"maker"` // the device registry's manufacturer: "Apple" on an iPhone
}

const (
	phoneIOS     = "ios"
	phoneAndroid = "android"
)

// platform: ios (Home Assistant's own test: the maker is Apple), android
// (any other maker: the Companion app exists for these two), or "" unknown.
func (t phoneTarget) platform() string {
	switch {
	case strings.EqualFold(strings.TrimSpace(t.Maker), "Apple"): // any doubt keeps iOS's bool: the unlock stays
		return phoneIOS
	case t.Maker != "":
		return phoneAndroid
	}
	return ""
}

// phoneSetup: who confirms (the linked user) and where (the linked phone,
// the only target), plus the owner (the links' key) and the Relay's own
// user, whose events never count.
type phoneSetup struct {
	owner, relayUser, confirmer string
	targets                     []phoneTarget
	why                         phoneWhy // phoneMissing: why
}

type phoneEvent struct {
	EventType string `json:"event_type"`
	Origin    string `json:"origin"`
	Data      struct {
		Action string `json:"action"`
	} `json:"data"`
	Context struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
	} `json:"context"`
}

// judgePhoneEvent: only an event carrying one of our ids decides, and only
// from the confirming user's own app.
func judgePhoneEvent(ev phoneEvent, ok, no string, setup phoneSetup) phoneAnswer {
	if ev.EventType != phoneEventType || ev.Data.Action != ok && ev.Data.Action != no {
		return phoneIgnored
	}
	user := ev.Context.UserID
	if ev.Origin != "REMOTE" || user == "" || user != setup.confirmer || user == setup.relayUser {
		return phoneRejected
	}
	if ev.Data.Action == ok {
		return phoneConfirmed
	}
	return phoneDeclined
}

func phoneNonce(kind string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "HANOVA_" + kind + "_" + hex.EncodeToString(b), nil
}

// askPhone sends the notification to the setup's phone (the linked one, or
// the one being linked) and waits for a decisive answer. The caller closes
// it with reply.say (the result replaces it); every caller defers one. The
// ids are never printed, logged, or stored, and die with ctx: a cancelled
// parent stops listening, so a later tap counts nowhere.
func askPhone(parent context.Context, s *relaySession, setup phoneSetup, tag string, ask phoneAsk, wait time.Duration, notice io.Writer) (answer phoneAnswer, reply phoneReply) {
	ok, errOK := phoneNonce("OK")
	no, errNo := phoneNonce("NO")
	attempt, errTag := phoneNonce("T")
	if errOK != nil || errNo != nil || errTag != nil {
		return phoneFailed, reply
	}
	// The person's wait is not read time: the caller's re-check after the
	// answer gets a fresh read budget (a tap after the session's 30 s was
	// read as "the phone changed"). Runs after the lanes' cancel.
	defer s.renewBudget()
	// A fresh tag per attempt: a late result of an earlier attempt can't replace this one.
	tag += "-" + attempt[len(attempt)-8:]
	collect, notify := phoneCollect, phoneNotify // the lanes may outlive this call
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	events, failed := make(chan phoneEvent, 16), make(chan error, 4)
	lane := func(delay time.Duration) {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		for ctx.Err() == nil {
			got, err := collect(ctx, s, phoneWindowSpan)
			if err != nil {
				select {
				case failed <- err:
				default:
				}
				select { // the other lane still listens
				case <-time.After(time.Second):
				case <-ctx.Done():
				}
				continue
			}
			for _, ev := range got {
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}
	// A short probe window proves the Relay can subscribe at all before any
	// notification goes out: buttons no listener can hear are never sent.
	if _, err := collect(ctx, s, time.Millisecond); err != nil {
		return phoneFailed, reply
	}
	// Two overlapping lanes: one re-subscribes while the other still listens.
	go lane(0)
	go lane(phoneWindowSpan / 2)
	select {
	case <-failed: // no listener: never send a tap no one can hear
		return phoneFailed, reply
	case <-time.After(phoneSettle):
	case <-ctx.Done():
		return phoneNoAnswer, reply
	}
	deadline, _ := ctx.Deadline()
	sent, sending := sendPhones(ctx, s, clearingFirst(phoneLastResults(s.paths), setup.owner, notify), setup.targets, func(t phoneTarget) map[string]any { return phonePayload(ask, t.platform(), tag, ok, no, deadline) })
	all := make([]string, 0, len(setup.targets))
	for _, target := range setup.targets {
		all = append(all, target.Service)
	}
	// Every phone: a send still in flight at the deadline may land later.
	// The result or the clear waits for an ask still in flight (bounded): a
	// late ask would land after it. The tag is kept either way, so the next
	// question clears whatever stays (a clear iOS didn't deliver).
	end := func(body map[string]any) {
		until := time.Now().Add(phoneTellSpan) // one bound for both: an MCP call stays under its 60 s
		select {
		case <-sending:
		case <-time.After(phoneTellSpan):
		}
		notePhoneResult(s.paths, setup.owner, all, tag)
		tellPhones(s, notify, all, body, until)
	}
	reply = phoneReply{once: &sync.Once{},
		tell: func(title, body string) { end(phoneResultPayload(title, body, tag)) },
		clear: func() {
			end(map[string]any{"message": "clear_notification", "data": map[string]any{"tag": tag, "priority": "high"}})
		},
	}
	if len(sent) == 0 {
		return phoneFailed, reply
	}
	if notice != nil {
		fmt.Fprintf(notice, phoneSentText+"\n", terminalSafeName(setup.targets[0].Name), phoneTapLine("en", setup.targets[0].platform(), ask.button))
	}
	var quiet <-chan time.Time
	if notice != nil {
		quiet = time.After(phoneNothingAfter)
	}
	seen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return phoneNoAnswer, reply
		case <-quiet:
			fmt.Fprintln(notice, phoneNothingHint(setup.targets[0].platform()))
		case <-failed:
		case ev := <-events:
			if ev.Context.ID != "" && seen[ev.Context.ID] { // both lanes saw it
				continue
			}
			seen[ev.Context.ID] = true
			if answer = judgePhoneEvent(ev, ok, no, setup); answer != phoneIgnored {
				return answer, reply
			}
		}
	}
}

// sendPhones sends to every phone in parallel, bounded by the wait: a
// stalled notify service never holds apply past the confirmation deadline.
// It returns the services that accepted it, and a channel closed once
// every send has finished (one may outlive the wait).
func sendPhones(ctx context.Context, s *relaySession, notify func(*relaySession, string, map[string]any) error, targets []phoneTarget, body func(phoneTarget) map[string]any) ([]string, <-chan struct{}) {
	accepted, sending := make(chan string, len(targets)), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(targets))
	go func() { wg.Wait(); close(sending) }()
	for _, target := range targets {
		go func() {
			defer wg.Done()
			if notify(s, target.Service, body(target)) == nil {
				accepted <- target.Service
				return
			}
			accepted <- ""
		}()
	}
	var sent []string
	for range targets {
		select {
		case service := <-accepted:
			if service != "" {
				sent = append(sent, service)
			}
		case <-ctx.Done():
			return sent, sending
		}
	}
	sort.Strings(sent)
	return sent, sending
}

// tellPhones sends body to every phone the ask reached, in parallel and for
// at most until (best effort: a stale ask has dead buttons, since its ids
// died with the wait; a slow Relay never delays apply).
func tellPhones(s *relaySession, notify func(*relaySession, string, map[string]any) error, services []string, body map[string]any, until time.Time) {
	done := make(chan struct{}, len(services))
	for _, service := range services {
		go func() {
			_ = notify(s, service, body)
			done <- struct{}{}
		}()
	}
	timeout := time.After(time.Until(until))
	for range services {
		select {
		case <-done:
		case <-timeout:
			return
		}
	}
}

// collectPhoneEvents is one bounded listening window on the Relay: it
// returns at the first notification action or after span (≤ the Relay's
// 10 s cap; the window starts only once Home Assistant acknowledged the
// subscription, and a failed subscription is an error).
func collectPhoneEvents(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
	body, _ := json.Marshal(map[string]any{
		"message":        map[string]any{"type": "subscribe_events", "event_type": phoneEventType},
		"collect_events": map[string]any{"max_events": 1, "timeout_ms": max(span.Milliseconds(), 1), "on_limit": "return"},
	})
	data, err := s.sendIn(ctx, "ws", body, span+5*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Events []phoneEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

// confirmByPhone runs before apply for a phone plan, like confirmByWindow:
// on Confirm it hands apply the plan's code in process (humanAnswer). A
// phone plan never falls back to another channel.
func confirmByPhone(paths runtimePaths, opts verbOptions, id string, noSnapshot bool) (verbEnvelope, int, bool) {
	if chatCall != nil { // ha-nova serve asks only in its background job (chatApply); apply then refuses the plan
		return verbEnvelope{}, 0, false
	}
	a, env, exit, asked := startPhoneAsk(paths, opts, id, noSnapshot)
	if a == nil {
		return env, exit, asked
	}
	a.wait(os.Stderr)
	return a.finish()
}

// phoneAsking is a phone plan between its checks and the answer: ha-nova
// serve waits for it outside the verb lock (mcp_serve_phone.go).
type phoneAsking struct {
	id       string
	plan     storedPlan
	s        *relaySession
	idHex    string
	goal     actionGoal
	setup    phoneSetup
	ask      phoneAsk
	deadline time.Time
	answer   phoneAnswer
	reply    phoneReply
}

// startPhoneAsk checks a phone plan before its ask. nil: nothing to ask
// (asked=false: apply runs and reports; asked=true: env says why not).
func startPhoneAsk(paths runtimePaths, opts verbOptions, id string, noSnapshot bool) (*phoneAsking, verbEnvelope, int, bool) {
	plan, _, s, idHex, wait, goal, ok := humanPlan(paths, opts, id, noSnapshot, channelPhone)
	if !ok {
		return nil, verbEnvelope{}, 0, false
	}
	stop := func(code, message, next string) (*phoneAsking, verbEnvelope, int, bool) {
		phoneApplied = func(verbEnvelope, int) { s.close() } // apply closes the session after its output
		env, exit, asked := windowStopped(s, "refused", code, message, next)
		return nil, env, exit, asked
	}
	setup, status, deadline, inTime := discoverInTime(s, wait)
	if !inTime {
		return stop("PLAN_EXPIRED", "plan "+id+" expires before the phone could get an answer", replan)
	}
	if status != phoneReady {
		return stop("CONFIRM_UNAVAILABLE", phoneMissingWhy, phoneMissingStep())
	}
	return &phoneAsking{id: id, plan: plan, s: s, idHex: idHex, goal: goal, setup: setup, ask: phonePlanAsk(textLang(plan), goal, noSnapshot), deadline: deadline}, verbEnvelope{}, 0, false
}

// wait sends the notification and waits for the answer; notice hears the
// "sent to" line (nil: nobody).
func (a *phoneAsking) wait(notice io.Writer) {
	a.answer, a.reply = askPhone(context.Background(), a.s, a.setup, "ha-nova-"+a.plan.ID, a.ask, time.Until(a.deadline), notice)
}

// refuse: the tap came, but the bridge's policy changed meanwhile; the
// phone hears that nothing changed.
func (a *phoneAsking) refuse(code, message, next string) (verbEnvelope, int, bool) {
	s, reply, lang := a.s, a.reply, textLang(a.plan)
	prefix, line := phoneNothing(lang, phoneRejected)
	phoneApplied = func(verbEnvelope, int) { reply.say(phoneResultTitle(lang, prefix, a.goal), line); s.close() }
	return windowStopped(s, "refused", code, message, next)
}

// finish turns the answer into apply's input: on Confirm the code in
// process, else why nothing was sent. The phone hears how it ended once
// apply has printed (phoneApplied).
func (a *phoneAsking) finish() (env verbEnvelope, exit int, asked bool) {
	s, id, reply, lang := a.s, a.id, a.reply, textLang(a.plan)
	handed := false
	defer func() {
		if !handed {
			prefix, line := phoneNothing(lang, a.answer)
			phoneApplied = func(verbEnvelope, int) { reply.say(phoneResultTitle(lang, prefix, a.goal), line); s.close() }
		}
	}()
	switch a.answer {
	case phoneConfirmed:
		switch still, unread := phoneStillLinked(s, a.setup); {
		case unread:
			return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't re-check the linked phone in Home Assistant after the tap",
				fmt.Sprintf("Tell the user nothing changed: their tap arrived, but HA NOVA couldn't re-check the linked phone in Home Assistant, so nothing ran. Run ha-nova apply --plan %s again (a new notification goes to their phone) only after they say they still want it; never ask them for a code.", id))
		case !still:
			return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the phone that confirmed is no longer the linked phone, or its user is no longer an administrator", phoneMissingStep())
		}
		humanAnswer, humanWhere = confirmCode(s.transport.Credential, a.idHex, a.plan.Channel, a.plan.ExpiresAt), channelPhone
		handed, phoneApplied = true, func(env verbEnvelope, exit int) {
			prefix, line := phoneApplyResult(lang, env, exit)
			reply.say(phoneResultTitle(lang, prefix, a.goal), line)
			s.close()
		}
		return env, 0, false
	case phoneDeclined:
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation was cancelled on the phone",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case phoneRejected:
		return windowStopped(s, "refused", "CONFIRM_REJECTED", phoneRejectedWhy,
			"Tell the user nothing changed: "+phoneRejectedWhy+". Apply again only if they ask; never ask them for a code.")
	case phoneFailed:
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation could not be sent to the phone", phoneMissingStep())
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the phone confirmation got no answer in time",
		fmt.Sprintf("Tell the user nothing changed: “%s” was not tapped in time. Next time: %s A plain tap only opens the app.%s Run ha-nova apply --plan %s again (a new notification goes to their phone) only after they say they still want it; never ask them for a code. %s They can also make this change in Home Assistant itself.", a.ask.button, a.tapLine("en"), a.watchLine(), id, a.nothingLine()))
}

// tapLine: how to answer on the phone asked (startPhoneAsk asks only a ready one), in lang.
func (a *phoneAsking) tapLine(lang string) string {
	return phoneTapLine(lang, a.setup.targets[0].platform(), a.ask.button)
}

// nothingLine: where notifications turn on, for a person who saw none.
func (a *phoneAsking) nothingLine() string {
	return "If nothing showed on their phone, tell them where to turn on its notifications: " + phoneNothingSteps(a.setup.targets[0].platform())
}

// watchLine: an Apple Watch hears a tap only once its app has opened.
func (a *phoneAsking) watchLine() string {
	if a.setup.targets[0].platform() == phoneAndroid {
		return ""
	}
	return " If they tapped on an Apple Watch, they open the Home Assistant app on the watch once."
}
