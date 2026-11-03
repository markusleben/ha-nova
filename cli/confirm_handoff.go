package main

import (
	"context"
	"errors"
	"reflect"
)

// After a window or phone Confirm, apply takes over the Link route the ask
// logged in to, and a failure that sent nothing says that the human had
// confirmed (docs/work/2026-11-03-link-route-timeouts-spec.md, Follow-up).

// askedRoute: the session a window or phone ask in this process confirmed
// with; it lives and is reset with humanAnswer.
var askedRoute *relaySession

// newApplySession opens apply's session. It takes over askedRoute when that
// is Link, once, for the same server and the very configuration the ask
// logged in with: a late Confirm needs no second route choice or login.
// Every other route is chosen again.
func newApplySession(paths runtimePaths, opts verbOptions, id string) (*relaySession, error) {
	asked := askedRoute
	askedRoute = nil
	if asked != nil && asked.transport.Via == relayViaCloud {
		setServerSelectionOverride(opts.server)
		if cfg, err := loadConfig(paths); err == nil && activeServerProfile() == asked.server && reflect.DeepEqual(cfg, asked.cfg) {
			ctx, cancel := context.WithTimeout(context.Background(), relaySessionOverallTimeout)
			return &relaySession{paths: paths, cfg: cfg, transport: asked.transport, server: asked.server, ctx: ctx, cancel: cancel,
				slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{}}, nil
		}
	}
	s, err := newRelaySession(paths, opts)
	var failure *verbFailure
	if err != nil && humanAnswer != "" && !errors.As(err, &failure) {
		failure = nothingSent(err)
		confirmedNotSent(failure, id, true)
		return nil, failure
	}
	return s, err
}

// confirmedNotSent: the human confirmed in a window, on the phone, or in a
// form, but nothing reached Home Assistant. RELAY_ERROR would send the agent
// to setup or to a retry that asks the human again unprompted. kept: the
// plan is still open.
func confirmedNotSent(failure *verbFailure, id string, kept bool) {
	if humanAnswer == "" {
		return
	}
	again := "Run ha-nova apply --plan " + id + " again (it asks them again)."
	if !kept {
		again = replan
	}
	failure.code = "CONFIRMED_NOT_SENT"
	failure.next = "Nothing changed: the user confirmed, but the change never reached Home Assistant (the message says why). Read the target once with ha-nova get to check, then tell the user in plain words. Never apply or call it again on your own. Only after they say they still want it: " + again
}
