package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// The CLI's calls on the Relay's ask route and its gate answers (gate_client.go).

const gateWorkerStep = 20 * time.Second // the Relay's wait for one worker step (nova/src/runtime/gate-worker.ts)

// gatePollTimeout: one wait on an ask (the Relay answers within 25 s); tests shorten it.
var gatePollTimeout = 40 * time.Second

// gateOpenWait: how long opening an ask for n requests may take. The
// Relay's worker may spend a step on each request twice and two more
// (gate-worker.ts), within what is left of this command's wait (askWait;
// ha-nova mcp's job: the ask's life).
func gateOpenWait(n int) time.Duration {
	budget := askWait()
	if gateApproval != nil {
		budget = gateAskLife
	}
	return min(time.Duration(2*n+2)*gateWorkerStep+5*time.Second, budget)
}

// gateRefusal: the Relay's safety check refused the request before it
// forwarded anything; only GATE_ANSWER_WITHHELD came after Home Assistant ran it.
func gateRefusal(code string) bool {
	return strings.HasPrefix(code, "GATE_") && code != "GATE_ANSWER_WITHHELD"
}

// gateRefusalPeek reads the head of a Relay answer of 5xx (cloud_ingress.go):
// true when it is the Relay's own gate refusal, so nothing was forwarded.
// The bytes read go back in front of the body.
func gateRefusalPeek(body io.ReadCloser) (io.ReadCloser, bool) {
	head, _ := io.ReadAll(io.LimitReader(body, 8<<10))
	var envelope struct {
		OK    *bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	refused := json.Unmarshal(head, &envelope) == nil && envelope.OK != nil && !*envelope.OK && gateRefusal(envelope.Error.Code)
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), body), body}, refused
}

// gateCall sends one request to the ask route within wait, never twice (a
// second open would replace the first), and decodes its data into into.
func (s *relaySession) gateCall(parent context.Context, wait time.Duration, body map[string]any, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	req, err := newRelayRequest(ctx, s.transport, gateAskRoute, raw)
	if err != nil {
		return err
	}
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	var envelope struct {
		OK    *bool           `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	switch {
	case err != nil:
		return err
	case json.Unmarshal(data, &envelope) != nil || envelope.OK == nil:
		return &relayCallError{endpoint: "gate", status: resp.StatusCode, message: "not a result envelope"}
	case !*envelope.OK:
		return &relayCallError{endpoint: "gate", status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	return json.Unmarshal(envelope.Data, into)
}

// gateCancel ends the ask on the Relay, best effort (planAsks may be done).
// Its answer never sends anything, an approval that came first included.
func (s *relaySession) gateCancel(askID string) {
	var ended struct{}
	_ = s.gateCall(context.Background(), 5*time.Second, map[string]any{"ask_id": askID, "op": "cancel"}, &ended)
}
