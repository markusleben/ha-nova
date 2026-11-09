package main

import (
	"context"
	"strings"
	"time"
)

// A link from the Relay (POST /gate/v1/phone, docs/work/2026-11-03-relay-typed-gate-spec.md §3):
// the phone linked now, if any, confirms the move first, then the new phone
// confirms the link. The worker words both asks from confirm_strings in
// Home Assistant's language; the new phone's name is the one Home Assistant
// lists, never the device's. Both taps share the request's wait.

func (in gatePhoneRequest) valid() bool {
	wait := time.Duration(in.WaitMS) * time.Millisecond
	link := func(l phoneLink) bool {
		return phoneServicePattern.MatchString(l.Service) && l.DeviceID != "" && l.UserID != ""
	}
	_, known := confirmCatalogs[in.Lang]
	text := known && in.Title != "" && in.Body != "" && in.Button != "" && in.Words == "" && in.Device == "" && in.Current == nil
	words := in.Words == "link" && in.Device != "" && in.Lang+in.Title+in.Body+in.Button == "" && (in.Current == nil || link(*in.Current))
	return (text || words) && link(in.Link) && wait >= time.Second && wait <= gatePhoneMaxWait
}

func gatePhoneLink(ctx context.Context, paths runtimePaths, in gatePhoneRequest) (gatePhoneAnswer, error) {
	deadline := time.Now().Add(time.Duration(in.WaitMS) * time.Millisecond)
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		return gatePhoneAnswer{}, err
	}
	defer s.close()
	lang := s.homeLanguage()
	setup, status := gatePhoneSetup(s, in.Link)
	if status != phoneReady {
		return gatePhoneAnswer{Answer: "unavailable", Why: gatePhoneWhy(setup.why)}, nil
	}
	requested := confirmT(lang, "phone.requested_by", "host", sayText(in.Device, false))
	if in.Current != nil {
		current, status := gatePhoneSetup(s, *in.Current)
		if status != phoneReady {
			return gatePhoneAnswer{Answer: "unavailable", Why: gatePhoneWhy(current.why)}, nil
		}
		move := phoneAsk{lang: lang, title: confirmT(lang, "phone.move.title"), button: confirmT(lang, "phone.move.button"), unlock: true,
			body: confirmT(lang, "phone.move.body", "phone", sayText(setup.targets[0].Name, true), "requested", requested)}
		if answer := gatePhoneTap(ctx, s, paths, *in.Current, current, move, time.Until(deadline)); answer.Answer != "approved" {
			return answer, nil
		}
	}
	link := phoneAsk{lang: lang, title: phoneLinkTitle(lang), body: requested, button: phoneLinkButton(lang), unlock: true}
	return gatePhoneTap(ctx, s, paths, in.Link, setup, link, time.Until(deadline)), nil
}

// gatePhoneTap asks one phone and judges its tap. The question never stays
// answerable: the result replaces it, or, when the Relay hung up (the ask
// ended on the NOVA page, on the computer, or a newer ask replaced it; the
// worker can't tell which), a quiet note and a clear withdraw it.
func gatePhoneTap(ctx context.Context, s *relaySession, paths runtimePaths, link phoneLink, setup phoneSetup, ask phoneAsk, wait time.Duration) gatePhoneAnswer {
	answer, reply := askPhone(ctx, s, setup, gatePhoneTag, ask, wait, nil)
	lang := ask.lang
	title := func(prefix string) string {
		return confirmT(lang, "phone.result_title", "prefix", prefix, "goal", strings.TrimSuffix(ask.title, "?"))
	}
	switch answer {
	case phoneConfirmed:
		still := gatePhoneStill(paths, link, setup)
		if still.Answer != "approved" {
			reply.say(title(confirmT(lang, "outcome.not_done")), confirmT(lang, "outcome.line.nothing_changed"))
			return still
		}
		reply.say(title(confirmT(lang, "outcome.approved")), confirmT(lang, "outcome.line.confirmed_computer"))
		return still
	case phoneDeclined:
		reply.say(title(confirmT(lang, "outcome.cancelled")), confirmT(lang, "outcome.line.nothing_changed"))
		return gatePhoneAnswer{Answer: "declined"}
	case phoneRejected:
		reply.say(title(confirmT(lang, "outcome.cancelled")), confirmT(lang, "outcome.line.other_user"))
		return gatePhoneAnswer{Answer: "rejected"}
	case phoneNoAnswer:
		if ctx.Err() != nil {
			reply.withdraw(title(confirmT(lang, "outcome.ended")), confirmT(lang, "outcome.line.ended_elsewhere"))
			return gatePhoneAnswer{Answer: "none"}
		}
		reply.say(title(confirmT(lang, "outcome.not_confirmed")), confirmT(lang, "outcome.line.no_answer"))
		return gatePhoneAnswer{Answer: "none"}
	}
	return gatePhoneAnswer{Answer: "unavailable", Why: "send"}
}
