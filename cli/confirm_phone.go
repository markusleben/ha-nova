package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Channel B (docs/work/2026-10-17-human-confirmation-channels-spec.md §7):
// a Companion-app notification on the Home Assistant owner's phones with
// Confirm and Cancel. The action ids are random nonces that live only in
// this process; the tap counts only when Home Assistant reports it from
// the owner's own app (context.user_id = owner, origin REMOTE), never from
// the Relay's own user. Where the Relay acts as the owner (a standalone
// Relay on the owner's token) that can't be told apart, so B is
// unavailable there.

type phoneAnswer int

const (
	phoneIgnored   phoneAnswer = iota // not ours: keep waiting
	phoneNoAnswer                     // timed out: nothing is sent
	phoneConfirmed                    // the owner tapped Confirm
	phoneDeclined                     // the owner tapped Cancel
	phoneRejected                     // one of our ids came from someone else: stop, nothing is sent
	phoneFailed                       // the notification could not be sent: nothing is sent, no fallback
)

const (
	phoneEventType  = "mobile_app_notification_action"
	phoneSentText   = "HA NOVA sent a confirmation to your phone — tap Confirm there."
	phoneAsked      = "Requested by your AI assistant. Confirm only if you want this."
	phoneWindowSpan = 10 * time.Second                      // the Relay's collect_events cap; two lanes overlap by half
	phoneMaxMessage = 600                                   // runes: a lock-screen summary, not the whole preview
	phoneTitle      = "HA NOVA: hold or expand for Confirm" // a plain tap only opens the app
	phoneClearSpan  = 3 * time.Second                       // cleanup never delays apply longer
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
	Service string `json:"service"` // notify service: mobile_app_<slug>
	Name    string `json:"name"`
}

// phoneSetup: who confirms (the owner) and where (their phones), plus the
// Relay's own user, whose events never count.
type phoneSetup struct {
	owner, relayUser string
	targets          []phoneTarget
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
// from the owner's own app.
func judgePhoneEvent(ev phoneEvent, ok, no string, setup phoneSetup) phoneAnswer {
	if ev.EventType != phoneEventType || ev.Data.Action != ok && ev.Data.Action != no {
		return phoneIgnored
	}
	user := ev.Context.UserID
	if ev.Origin != "REMOTE" || user == "" || user != setup.owner || user == setup.relayUser {
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

// askPhone sends the notification to every phone, waits for the first
// decisive answer, and then clears it everywhere. The ids are never
// printed, logged, or stored.
func askPhone(s *relaySession, setup phoneSetup, tag, message string, wait time.Duration) phoneAnswer {
	ok, errOK := phoneNonce("OK")
	no, errNo := phoneNonce("NO")
	attempt, errTag := phoneNonce("T")
	if errOK != nil || errNo != nil || errTag != nil {
		return phoneFailed
	}
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
			got, err := collect(ctx, s)
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
	var sent []string
	for _, target := range setup.targets {
		if notify(s, target.Service, phonePayload(message, tag, ok, no, deadline)) == nil {
			sent = append(sent, target.Service)
		}
	}
	defer clearPhones(s, notify, sent, tag)
	if len(sent) == 0 {
		return phoneFailed
	}
	fmt.Fprintln(os.Stderr, phoneSentText)
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

// phonePayload: Confirm needs an unlocked phone (Face ID, fingerprint, or
// passcode; iOS, Android 12+) and is styled destructive (iOS); the
// notification expires with the wait (Android timeout, iOS apns-expiration).
func phonePayload(message, tag, ok, no string, deadline time.Time) map[string]any {
	seconds := max(int(time.Until(deadline).Seconds()), 1)
	return map[string]any{"title": phoneTitle, "message": message, "data": map[string]any{
		"tag": tag,
		"actions": []any{
			map[string]any{"action": ok, "title": "Confirm", "destructive": true, "authenticationRequired": true},
			map[string]any{"action": no, "title": "Cancel"},
		},
		"push":         map[string]any{"interruption-level": "time-sensitive"},
		"apns_headers": map[string]any{"apns-expiration": strconv.FormatInt(deadline.Unix(), 10)},
		"priority":     "high", "channel": "HA NOVA", "importance": "high", "timeout": seconds,
	}}
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

// phoneMessage: the plan's code-free text, short enough for a lock screen.
func phoneMessage(plan storedPlan, noSnapshot bool) string {
	text := []rune(confirmText(plan, noSnapshot))
	if len(text) > phoneMaxMessage {
		text = append(text[:phoneMaxMessage-1], '…')
	}
	return string(text) + "\n\n" + phoneAsked
}

// collectPhoneEvents is one bounded listening window on the Relay: it
// returns at the first notification action or after phoneWindowSpan.
func collectPhoneEvents(ctx context.Context, s *relaySession) ([]phoneEvent, error) {
	body, _ := json.Marshal(map[string]any{
		"message":        map[string]any{"type": "subscribe_events", "event_type": phoneEventType},
		"collect_events": map[string]any{"max_events": 1, "timeout_ms": phoneWindowSpan.Milliseconds(), "on_limit": "return"},
	})
	data, err := s.sendIn(ctx, "ws", body, phoneWindowSpan+5*time.Second)
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
	plan, fresh, s, idHex, wait, ok := humanPlan(paths, opts, id, noSnapshot, channelPhone)
	if !ok {
		return env, 0, false
	}
	defer s.close()
	deadline := time.Now().Add(wait) // discovery counts against the wait
	if wait < windowMinWait {
		return windowStopped(s, "refused", "PLAN_EXPIRED", "plan "+id+" expires before the phone could get an answer", replan)
	}
	setup, found := phoneDiscover(s)
	if !found {
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "this change is confirmed on the Home Assistant owner's phone, and HA NOVA can't reach it now", phoneMissingNext)
	}
	switch askPhone(s, setup, "ha-nova-"+plan.ID, phoneMessage(fresh, noSnapshot), time.Until(deadline)) {
	case phoneConfirmed:
		humanAnswer = confirmCode(s.transport.Credential, idHex, channelPhone, plan.ExpiresAt)
		return env, 0, false
	case phoneDeclined:
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation was cancelled on the phone",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case phoneRejected:
		return windowStopped(s, "refused", "CONFIRM_REJECTED", "the answer came from a device that isn't signed in to Home Assistant as the owner",
			"Tell the user nothing changed: the answer came from a device that isn't signed in to Home Assistant as the owner. Apply again only if they ask; never ask them for a code.")
	case phoneFailed:
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation could not be sent to the phone", phoneMissingNext)
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the phone confirmation got no answer in time",
		fmt.Sprintf("Tell the user nothing changed: Confirm was not pressed in time (hold or expand the notification to see it; a plain tap only opens the app). Run ha-nova apply --plan %s again (a new notification goes to their phone) only after they say they still want it; never ask them for a code. If no notification arrived, they can make this change in Home Assistant itself.", id))
}

const phoneMissingNext = "Tell the user nothing changed: HA NOVA could not reach their phone. They can make this change in Home Assistant itself, or try again later; never ask them for a code."

// humanPlan reads a typed plan of channel and re-plans it like apply, for
// a window or phone ask. ok=false: apply runs and reports whatever is
// wrong (drift, expiry, a refusal); s is open only when ok.
func humanPlan(paths runtimePaths, opts verbOptions, id string, noSnapshot bool, channel string) (plan, fresh storedPlan, s *relaySession, idHex string, wait time.Duration, ok bool) {
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
		return plan, fresh, nil, "", 0, false
	}
	if !planSealed(plan, s.transport.Credential) { // an edited plan file: no ask, and apply refuses it
		s.close()
		return plan, fresh, nil, "", 0, false
	}
	fresh, ok = replanStored(paths, s, &verbEnvelope{}, plan, channel)
	idHex = planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce)
	if !ok || fresh.Hash != plan.Hash || fresh.Tier != plan.Tier || plan.ID != "p_"+idHex[:12] || fresh.Prompt == "" {
		s.close()
		return plan, fresh, nil, "", 0, false
	}
	return plan, fresh, s, idHex, min(windowWait, expires.Sub(verbNow())), true
}
