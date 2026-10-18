package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	phoneEventType  = "mobile_app_notification_action"
	phoneWindowSpan = 10 * time.Second // the Relay's collect_events cap; two lanes overlap by half
	phoneClearSpan  = 3 * time.Second  // cleanup never delays apply longer
)

var (
	phoneSettle = time.Second // a failing subscription returns at once; a working one has acked by then
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
// the one being linked), waits for a decisive answer, and then clears it.
// The ids are never printed, logged, or stored.
func askPhone(s *relaySession, setup phoneSetup, tag string, ask phoneAsk, wait time.Duration) phoneAnswer {
	ok, errOK := phoneNonce("OK")
	no, errNo := phoneNonce("NO")
	attempt, errTag := phoneNonce("T")
	if errOK != nil || errNo != nil || errTag != nil {
		return phoneFailed
	}
	// The person's wait is not read time: the caller's re-check after the
	// answer gets a fresh read budget (a tap after the session's 30 s was
	// read as "the phone changed"). Runs after the clear and the lanes' cancel.
	defer s.renewBudget()
	// A fresh tag per attempt: a late clear of an earlier attempt can't remove this one.
	tag += "-" + attempt[len(attempt)-8:]
	collect, notify := phoneCollect, phoneNotify // the lanes may outlive this call
	ctx, cancel := context.WithTimeout(context.Background(), wait)
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
		return phoneFailed
	}
	// Two overlapping lanes: one re-subscribes while the other still listens.
	go lane(0)
	go lane(phoneWindowSpan / 2)
	select {
	case <-failed: // no listener: never send a tap no one can hear
		return phoneFailed
	case <-time.After(phoneSettle):
	case <-ctx.Done():
		return phoneNoAnswer
	}
	deadline, _ := ctx.Deadline()
	sent := sendPhones(ctx, s, notify, setup.targets, phonePayload(ask, tag, ok, no, deadline))
	all := make([]string, 0, len(setup.targets))
	for _, target := range setup.targets {
		all = append(all, target.Service)
	}
	defer clearPhones(s, notify, all, tag) // every phone: a send still in flight at the deadline may land later
	if len(sent) == 0 {
		return phoneFailed
	}
	fmt.Fprintf(os.Stderr, phoneSentText+"\n", terminalSafeName(setup.targets[0].Name), ask.button)
	seen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return phoneNoAnswer
		case <-failed:
		case ev := <-events:
			if ev.Context.ID != "" && seen[ev.Context.ID] { // both lanes saw it
				continue
			}
			seen[ev.Context.ID] = true
			if answer := judgePhoneEvent(ev, ok, no, setup); answer != phoneIgnored {
				return answer
			}
		}
	}
}

// sendPhones sends to every phone in parallel, bounded by the wait: a
// stalled notify service never holds apply past the confirmation deadline.
// It returns the services that accepted it.
func sendPhones(ctx context.Context, s *relaySession, notify func(*relaySession, string, map[string]any) error, targets []phoneTarget, body map[string]any) []string {
	accepted := make(chan string, len(targets))
	for _, target := range targets {
		go func() {
			if notify(s, target.Service, body) == nil {
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
			return sent
		}
	}
	sort.Strings(sent)
	return sent
}

// clearPhones removes the notification from every phone it reached, in
// parallel and for at most phoneClearSpan (best effort: a stale one has dead
// buttons, since its ids died with the wait; a slow Relay never delays apply).
func clearPhones(s *relaySession, notify func(*relaySession, string, map[string]any) error, services []string, tag string) {
	done := make(chan struct{}, len(services))
	for _, service := range services {
		go func() {
			_ = notify(s, service, map[string]any{"message": "clear_notification", "data": map[string]any{"tag": tag}})
			done <- struct{}{}
		}()
	}
	timeout := time.After(phoneClearSpan)
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
func confirmByPhone(paths runtimePaths, opts verbOptions, id string, noSnapshot bool) (env verbEnvelope, exit int, asked bool) {
	plan, _, s, idHex, wait, goal, ok := humanPlan(paths, opts, id, noSnapshot, channelPhone)
	if !ok {
		return env, 0, false
	}
	defer s.close()
	deadline := time.Now().Add(wait) // discovery counts against the wait
	if wait < windowMinWait {
		return windowStopped(s, "refused", "PLAN_EXPIRED", "plan "+id+" expires before the phone could get an answer", replan)
	}
	setup, status := phoneDiscover(s)
	if status != phoneReady {
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", phoneMissingWhy, phoneMissingNext)
	}
	switch askPhone(s, setup, "ha-nova-"+plan.ID, phonePlanAsk(goal, noSnapshot), time.Until(deadline)) {
	case phoneConfirmed:
		switch still, unread := phoneStillLinked(s, setup); {
		case unread:
			return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't re-check the linked phone in Home Assistant after the tap",
				fmt.Sprintf("Tell the user nothing changed: HA NOVA couldn't re-check their linked phone after the tap. Run ha-nova apply --plan %s again (a new notification goes to their phone) only after they say they still want it; never ask them for a code.", id))
		case !still:
			return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the phone that confirmed is no longer the linked phone, or its user is no longer an administrator", phoneMissingNext)
		}
		humanAnswer = confirmCode(s.transport.Credential, idHex, channelPhone, plan.ExpiresAt)
		return env, 0, false
	case phoneDeclined:
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation was cancelled on the phone",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case phoneRejected:
		return windowStopped(s, "refused", "CONFIRM_REJECTED", phoneRejectedWhy,
			"Tell the user nothing changed: "+phoneRejectedWhy+". Apply again only if they ask; never ask them for a code.")
	case phoneFailed:
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation could not be sent to the phone", phoneMissingNext)
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the phone confirmation got no answer in time",
		fmt.Sprintf("Tell the user nothing changed: the phone's button was not tapped in time (hold or expand the notification to see it; a plain tap only opens the app). Run ha-nova apply --plan %s again (a new notification goes to their phone) only after they say they still want it; never ask them for a code. If no notification arrived, they can make this change in Home Assistant itself.", id))
}

// phoneStillLinked re-reads the link after a tap: unlinked, replaced, or its
// user demoted while the notification was open, the tap no longer counts.
func phoneStillLinked(s *relaySession, setup phoneSetup) (linked, unread bool) {
	again, status := phoneDiscover(s)
	if status == phoneMissing && again.why == whyRead { // unknown, never "changed"
		return false, true
	}
	return status == phoneReady && again.confirmer == setup.confirmer && len(setup.targets) == 1 &&
		again.targets[0].DeviceID == setup.targets[0].DeviceID, false
}

const (
	phoneMissingWhy  = "this change is confirmed on the phone linked to HA NOVA, and HA NOVA can't reach it now"
	phoneMissingNext = "Tell the user nothing changed: HA NOVA could not reach their linked phone. `ha-nova setup confirm-phone list` (it sends nothing; in a terminal if you have no shell) says why and what to do. They can also make this change in Home Assistant itself, or try again later. Never ask them for a code."
	phoneRejectedWhy = "the tap came from a device signed in to Home Assistant as someone other than the person whose phone is linked"
	// #719's pin: no phone chosen yet. The agent may run the listing itself;
	// only a tap on the chosen phone links it.
	phoneUnlinkedWhy  = "HA NOVA now confirms on one phone the user chooses, and none is chosen on this computer yet"
	phoneUnlinkedNext = "Tell the user nothing changed: HA NOVA now asks on one phone they choose. With a shell: run ha-nova setup confirm-phone list (it sends nothing), ask which phone is theirs, tell them to watch it for “Link this phone to HA NOVA?” and tap “Link”, then run ha-nova setup confirm-phone \"<that phone's name>\" (it waits up to 90 seconds) and plan again. Without a shell, they run ha-nova setup confirm-phone in a terminal. Never ask them for a code."
)

// humanPlan reads a typed plan of channel and re-plans it like apply, for
// a window or phone ask (goal: the phone's verb and object). ok=false:
// apply runs and reports whatever is wrong (drift, expiry, a refusal); s is
// open only when ok.
func humanPlan(paths runtimePaths, opts verbOptions, id string, noSnapshot bool, channel string) (plan, fresh storedPlan, s *relaySession, idHex string, wait time.Duration, goal [2]string, ok bool) {
	if humanAnswer != "" || !planIDPattern.MatchString(id) { // a form already answered
		return
	}
	plan, err := readPlanFile(filepath.Join(planDir(paths), id+".json"))
	expires, timeErr := time.Parse(time.RFC3339, plan.ExpiresAt)
	if err != nil || timeErr != nil || plan.ID != id || plan.Tier != tierTyped.String() || plan.Channel != channel ||
		plan.SchemaVersion != planSchemaVersion || !verbNow().Before(expires) || expires.After(verbNow().Add(planTTL+time.Minute)) ||
		opts.serverSet && opts.server != plan.Server ||
		noSnapshot && !plan.SnapshotFailed {
		return
	}
	opts.server, opts.serverSet = plan.Server, true
	if s, err = newRelaySession(paths, opts); err != nil {
		return plan, fresh, nil, "", 0, goal, false
	}
	if !planSealed(plan, s.transport.Credential) { // an edited plan file: no ask, and apply refuses it
		s.close()
		return plan, fresh, nil, "", 0, goal, false
	}
	env := verbEnvelope{}
	fresh, ok = replanStored(paths, s, &env, plan, channel)
	idHex = planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce)
	if !ok || fresh.Hash != plan.Hash || fresh.Tier != plan.Tier || plan.ID != "p_"+idHex[:12] || fresh.Prompt == "" {
		s.close()
		return plan, fresh, nil, "", 0, goal, false
	}
	return plan, fresh, s, idHex, min(windowWait, expires.Sub(verbNow())), env.ask, true
}
