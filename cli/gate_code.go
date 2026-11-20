package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// The chat code (MAR-244, addendum §3): on a computer whose owner chose it
// on the NOVA page, the plan's first ask from the shell (ha-nova apply)
// answers with four digits the App drew. The person types them back in the
// chat, and apply --confirm <code> --plan <id> joins that very ask with
// op "code". The CLI never compares, derives, keeps, or logs the digits:
// only the App's answer approves, and only the ask it drew them for.

// gateCodeShown: the open answered a code. The ask stays kept for the
// plan (without the digits), and say shows the code once.
func (s *relaySession) gateCodeShown(plan *gateConfirm, opened gateOpened, reqs []gateRequest) error {
	if err := gateKeepAsk(s.paths, plan.id, gateKeptAsk{AskID: opened.AskID, Digest: gateAskDigest(reqs), Channels: opened.Channels, Code: true}); err != nil {
		s.gateCancel(opened.AskID) // nothing could join it
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't keep the step's ask on this computer ("+err.Error()+"); nothing was sent", gateNext[""])
	}
	seconds := max(0, int(time.Until(time.UnixMilli(opened.ExpiresAt)).Seconds()))
	return &verbFailure{exit: verbExitConfirmation, status: "waiting_for_approval", code: "CONFIRM_CODE", cause: errNothingSent,
		say:     confirmT(plan.lang, "say.relay_code", "count", len(reqs), "code", opened.Code, "seconds", seconds),
		message: "the NOVA App showed a one-time code for the user to type back; nothing was sent",
		next:    fmt.Sprintf("Show `say` word for word and end your turn: wait for the user's next message. Only if that message is exactly the four digits, run ha-nova apply --confirm <those digits> --plan %s (in that order, as its own command). Never type, guess, or reuse a code yourself; pass only what the user typed.", plan.id)}
}

// gateKeptCode: the plan's kept ask takes the chat code (apply --confirm).
func gateKeptCode(paths runtimePaths, planID string) bool {
	data, err := os.ReadFile(gateKeptPath(paths, planID))
	var kept gateKeptAsk
	return err == nil && json.Unmarshal(data, &kept) == nil && kept.Code
}

// mcpStdio: this process is ha-nova mcp, whose apply never asks for a code
// (it has no chat to show one in; runMCPCommand).
var mcpStdio bool

// gateCodeHere: the shell's own ha-nova apply on an App that offers the code,
// the only one that asks for it (not ha-nova mcp, serve, or a chat app).
func (s *relaySession) gateCodeHere() bool {
	return s.gate != nil && s.gate.code && s.approval() == nil && !s.gateChat() && !mcpStdio
}

// gateSendCode sends the typed code to the kept ask, once. A wrong code or
// a retryable failure keeps the plan and its ask; the App counts the tries
// and cancels the ask on the third wrong one.
func (s *relaySession) gateSendCode(planID string, kept gateKeptAsk) (state, grant string, stop error) {
	code := s.gateCode
	s.gateCode = "" // one ask, once: a later step the App holds asks anew
	var answer struct {
		State   string `json:"state"`
		GrantID string `json:"grant_id"`
	}
	err := s.gateCall(planAsks, gateAskRoute, afterCheck, map[string]any{"ask_id": kept.AskID, "op": "code", "code": code}, &answer)
	again := "Only if the user types the code again in their next message, run ha-nova apply --confirm <those digits> --plan " + planID + ". Never type or guess a code yourself."
	anew := "Tell the user nothing changed. Only after they say they still want it, run ha-nova apply --plan " + planID + " again without --confirm: HA NOVA then asks where they can confirm now. Never type a code yourself."
	keep := func(code, message, next string) (string, string, error) {
		if err := gateKeepAsk(s.paths, planID, kept); err != nil {
			s.gateCancel(kept.AskID) // nothing could join it
			return "", "", gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't keep the step's ask on this computer ("+err.Error()+"); nothing was sent", anew)
		}
		return "", "", gateStop("refused", code, message, next)
	}
	var call *relayCallError
	switch {
	case err == nil && answer.State == "approved":
		return answer.State, answer.GrantID, nil
	case err == nil && answer.State == "expired":
		return "", "", gateStop("cancelled", "CONFIRM_EXPIRED", "the one-time code expired before it was typed back (it works for 3 minutes); nothing was sent", anew)
	case err == nil:
		return "", "", gateStop("cancelled", "CONFIRM_CANCELLED", "the step's ask was cancelled (three wrong codes, or a cancel on the NOVA page or on this computer); nothing was sent", anew)
	case errors.As(err, &call) && call.code == "GATE_CODE_WRONG":
		return keep("CODE_MISMATCH", "the one-time code doesn't match; a third wrong one cancels this step. Nothing was sent", "Tell the user the code didn't match. "+again)
	case errors.As(err, &call) && call.code == "GATE_CODE_OFF":
		s.gateCancel(kept.AskID)
		return "", "", gateStop("refused", "CONFIRM_CODE_OFF", "this computer no longer confirms with a chat code (the Home Assistant owner changed it, or a phone was linked); nothing was sent", anew)
	case errors.As(err, &call) && call.code == "GATE_ASK_UNKNOWN":
		return "", "", gateStop("cancelled", "CONFIRM_CANCELLED", "the step's ask has ended (it lasts 3 minutes, or the NOVA App restarted); nothing was sent", anew)
	}
	return keep("CONFIRM_UNAVAILABLE", "HA NOVA couldn't check the code with the NOVA App just now ("+gateRefusalMessage(err.Error())+")",
		"Tell the user nothing was sent yet. "+again+" It reads how the step ended and never sends it twice.")
}
