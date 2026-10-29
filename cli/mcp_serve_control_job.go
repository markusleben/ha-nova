package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"
)

// The bridge's control jobs (mcp_serve_control.go): writing a user's link,
// and the asks of a move or a stop.

var (
	errPolicyChanged  = errors.New("the user's chat-app access changed meanwhile")
	errSettingsUnread = errors.New("the chat-app settings are unreadable")
)

// The chat-app access a write still needs: allowed (stop), allowed at a
// level with a phone (adopt, move: Use the home or more), or no longer
// allowed (drop).
func chatAllowed(p *chatPolicy) bool { return p != nil }
func chatPhoneOK(p *chatPolicy) bool { return p != nil && p.level >= levelUse }
func chatRemoved(p *chatPolicy) bool { return p == nil }

// controlWrite answers a change made without a question.
func (b *chatBridge) controlWrite(user string, prev *phoneLink, present bool, link *phoneLink, want func(*chatPolicy) bool) (int, map[string]any) {
	switch err := b.setLink(user, prev, present, link, want); {
	case errors.Is(err, errPhoneLinkChanged) && link != nil:
		return controlError(http.StatusConflict, "PHONE_EXISTS", "this user has a chat-app phone already; a move asks it first")
	case errors.Is(err, errPhoneLinkChanged):
		return controlError(http.StatusConflict, "PHONE_CHANGED", "the link changed meanwhile; nothing changed")
	case errors.Is(err, errPolicyChanged):
		return controlError(http.StatusConflict, "ACCESS_CHANGED", "this user's chat-app access changed meanwhile; nothing changed")
	case errors.Is(err, errSettingsUnread):
		return controlError(http.StatusServiceUnavailable, "SETTINGS_UNREADABLE", "HA NOVA chat-app settings are unreadable")
	case err != nil:
		return controlError(http.StatusServiceUnavailable, "LINKS_UNWRITABLE", "phone-links.json can't be read or written")
	}
	return http.StatusOK, map[string]any{"status": "done"}
}

// setLink stores user's link (nil removes it) only while their entry is
// still prev and their chat-app access still passes want, both checked
// under linksMu right before the write. The bridge is its only writer.
func (b *chatBridge) setLink(user string, prev *phoneLink, present bool, link *phoneLink, want func(*chatPolicy) bool) error {
	b.linksMu.Lock()
	defer b.linksMu.Unlock()
	byUser, ok := readBridgeLinks(b.linksFile())
	if !ok {
		return errors.New("phone-links.json is unreadable")
	}
	now, there := byUser[user]
	if !(&controlLook{prev: prev, present: present}).same(&controlLook{prev: now, present: there}) {
		return errPhoneLinkChanged
	}
	if err := b.policyStill(user, want); err != nil {
		return err
	}
	delete(byUser, user)
	if link != nil {
		byUser[user] = link
	}
	raw, err := json.Marshal(byUser)
	if err != nil {
		return err
	}
	return writeFileAtomic(b.linksFile(), raw, 0o600)
}

// policyStill: nil while user's chat-app access, with Home Assistant's
// users read now, still passes want.
func (b *chatBridge) policyStill(user string, want func(*chatPolicy) bool) error {
	switch policy, code, _ := b.policy(user, true); {
	case code == http.StatusServiceUnavailable:
		return errSettingsUnread
	case !want(policy):
		return errPolicyChanged
	}
	return nil
}

// chatPhoneLink: the link to t as Home Assistant lists it.
func chatPhoneLink(t phoneTarget) *phoneLink {
	return &phoneLink{Service: t.Service, DeviceID: t.DeviceID, UserID: t.UserID, Name: terminalSafeName(t.Name), Platform: t.platform()}
}

// runPhoneJob: the phone linked now OKs first (a move, a stop); a move
// then asks the new phone, whose tap must come from the user it is listed
// under. After each tap the bridge reads again: a move asks its new phone
// only while the user is still allowed, and the link is written (or a
// stop's removed) only while the entry is unchanged and the user's access
// still allows it.
func (b *chatBridge) runPhoneJob(job *controlJob, look *controlLook, chosen *phoneTarget, lang string, codes, use bool) {
	end := func(status string) {
		b.mu.Lock()
		job.status, job.ended = status, time.Now() // the step stays: the page names the phone that ended it
		b.mu.Unlock()
	}
	defer func() {
		if p := recover(); p != nil {
			b.log.Printf("control: a phone change stopped: %v", p)
			end("failed")
		}
		look.s.close()
	}()
	notDone := func(reply phoneReply, err error) { // nothing changed; the phone hears why
		line, status := confirmT(lang, "chat_phone.reply.failed"), "failed" // a read or the write failed
		switch {
		case errors.Is(err, errPhoneLinkChanged):
			line, status = confirmT(lang, "chat_phone.reply.changed"), "changed"
		case errors.Is(err, errPolicyChanged):
			line, status = confirmT(lang, "chat_phone.reply.settings"), "changed"
		}
		reply.say(confirmT(lang, "chat_phone.reply.not_done_title"), line)
		end(status)
	}
	if look.status == phoneReady {
		answer, reply := askPhone(context.Background(), look.s, look.current, "ha-nova-chat-setting", chatPhoneChangeAsk(lang, chosen, codes, use), controlAskWait, nil)
		if answer != phoneConfirmed {
			reply.say(phoneNothing(lang, answer))
			end(controlAnswer[answer])
			return
		}
		want := chatPhoneOK // a move: the new phone must still confirm for the user
		if chosen == nil {  // a stop: its question said what chat codes do then
			want = func(p *chatPolicy) bool { return p != nil && p.chatCodes() == codes }
		}
		err := b.policyStill(job.user, want) // first: the link reads as changed once the user may no longer confirm
		var still, unread bool
		if err == nil {
			unread = b.asUser(job.user, func() { still, unread = phoneStillLinked(look.s, look.current) }) != nil || unread
		}
		switch {
		case err != nil:
		case unread:
			err = errors.New("the link couldn't be read again")
		case !still:
			err = errPhoneLinkChanged
		case chosen == nil:
			err = b.setLink(job.user, look.prev, true, nil, want)
		}
		switch {
		case err != nil:
			notDone(reply, err)
			return
		case chosen == nil:
			reply.say(confirmT(lang, "chat_phone.reply.stopped_title"), confirmT(lang, "chat_phone.reply.stopped"))
			end("done")
			return
		}
		reply.say(confirmT(lang, "chat_phone.reply.approved_title"), confirmT(lang, "chat_phone.reply.approved", "phone", sayText(chosen.Name, true)))
	}
	link := chatPhoneLinkAsk(lang, use)
	b.step(job, "new", *chosen, chosen, link.button)
	ask := phoneSetup{owner: look.home.owner, relayUser: look.home.relay, confirmer: chosen.UserID, targets: []phoneTarget{*chosen}}
	answer, reply := askPhone(context.Background(), look.s, ask, "ha-nova-chat-link", link, controlAskWait, nil)
	if answer != phoneConfirmed {
		reply.say(phoneNothing(lang, answer))
		end(controlAnswer[answer])
		return
	}
	err := b.policyStill(job.user, chatPhoneOK) // first: removed or lowered meanwhile is a settings change
	var again *controlLook
	if err == nil {
		again, err = b.look(job.user, look.s)
	}
	listed := func(t phoneTarget) bool { return t.DeviceID == chosen.DeviceID && t.UserID == chosen.UserID }
	if err == nil && (!look.same(again) || !slices.ContainsFunc(again.home.eligible(job.user), listed)) {
		err = errPhoneLinkChanged
	}
	if err == nil {
		err = b.setLink(job.user, look.prev, look.present, chatPhoneLink(*chosen), chatPhoneOK)
	}
	if err != nil {
		notDone(reply, err)
		return
	}
	linked := confirmT(lang, "chat_phone.reply.linked")
	if use {
		linked = confirmT(lang, "chat_phone.reply.linked_use")
	}
	reply.say(confirmT(lang, "chat_phone.reply.linked_title"), linked)
	end("done")
}

var controlAnswer = map[phoneAnswer]string{phoneNoAnswer: "timeout", phoneDeclined: "declined", phoneRejected: "rejected", phoneFailed: "failed"}

// step: the phone a running job asks now, the button its question shows,
// and a move's new phone.
func (b *chatBridge) step(job *controlJob, step string, asked phoneTarget, to *phoneTarget, button string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job.step, job.phone, job.platform, job.button = step, terminalSafeName(asked.Name), asked.platform(), button
	if to != nil {
		job.to = terminalSafeName(to.Name)
	}
}

// chatPhoneChangeAsk goes to the phone linked now: move to chosen, or stop
// (chosen nil; its text says what chat apps do then). use: the user is
// below Manage everything, where a chat app deletes nothing, so the text
// names what asks the phone there.
func chatPhoneChangeAsk(lang string, chosen *phoneTarget, codes, use bool) phoneAsk {
	requested := " " + confirmT(lang, "chat_phone.requested")
	if chosen != nil {
		body := confirmT(lang, "chat_phone.move.body", "phone", sayText(chosen.Name, true))
		if use {
			body = confirmT(lang, "chat_phone.move.body_use", "phone", sayText(chosen.Name, true))
		}
		return phoneAsk{lang: lang, title: confirmT(lang, "chat_phone.move.title"), body: body + requested, button: confirmT(lang, "chat_phone.move.button"), unlock: true}
	}
	body := confirmT(lang, "chat_phone.stop.body_refused")
	switch {
	case codes:
		body = confirmT(lang, "chat_phone.stop.body_codes")
	case use:
		body = confirmT(lang, "chat_phone.stop.body_refused_use")
	}
	return phoneAsk{lang: lang, title: confirmT(lang, "chat_phone.stop.title"), body: body + requested, button: confirmT(lang, "chat_phone.stop.button"), unlock: true}
}

func chatPhoneLinkAsk(lang string, use bool) phoneAsk {
	body := confirmT(lang, "chat_phone.link.body")
	if use {
		body = confirmT(lang, "chat_phone.link.body_use")
	}
	return phoneAsk{lang: lang, title: confirmT(lang, "chat_phone.link.title"), body: body + " " + confirmT(lang, "chat_phone.requested"),
		button: confirmT(lang, "chat_phone.link.button"), unlock: true}
}
