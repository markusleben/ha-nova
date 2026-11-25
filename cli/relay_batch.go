package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Batched loaded-config reads (MAR-180,
// docs/work/2026-11-20-ws-batch-reads-spec.md): many automation/config and
// script/config reads in one POST /ws, so the listener census over Home
// Assistant Link waits for a few round trips instead of one per eight
// configs. Only on a Relay whose /health says ws_batch: 1; else single reads.

const wsBatchMax = 16

// wsBatchProbeTimeout: how long the census waits to learn whether it may
// batch before it reads one by one, as on an older App.
const wsBatchProbeTimeout = 5 * time.Second

var wsBatchEntityID = regexp.MustCompile(`^(automation|script)\.[a-z0-9_]+$`)

// wsBatchItem: a read a batch may carry, exactly {type, entity_id} of a
// loaded config whose entity id is of that domain (the gate rates the same).
func wsBatchItem(request map[string]any) bool {
	wsType, _ := request["type"].(string)
	entityID, _ := request["entity_id"].(string)
	domain, isConfig := strings.CutSuffix(wsType, "/config")
	return len(request) == 2 && isConfig && wsBatchEntityID.MatchString(entityID) && strings.HasPrefix(entityID, domain+".")
}

// wsBatch: whether the Relay takes batches (GET /health ws_batch: 1), read
// once per session. A failed or slow read means no.
func (s *relaySession) wsBatch() bool {
	s.batchOnce.Do(func() {
		s.mu.Lock()
		parent := s.ctx
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(parent, wsBatchProbeTimeout)
		defer cancel()
		var health struct {
			WSBatch int `json:"ws_batch"`
		}
		s.batchOn = s.readHealth(ctx, &health) == nil && health.WSBatch == 1
	})
	return s.batchOn
}

// readBatched sends the reads in requests that no earlier read made, in
// batches, and keeps each answer where s.read finds it, so each caller reads
// as before. Not for the App's own callers, whose reads the Relay doesn't
// redact, nor while a cut preview is replayed, nor on a Relay without batches.
// inFlight caps the batches sent at once (0: no cap).
func (s *relaySession) readBatched(requests []map[string]any, inFlight int) {
	if len(requests) < 2 || s.gateExempt() || s.replaying() || !s.wsBatch() {
		return
	}
	var items []json.RawMessage
	var memos []*relayMemo
	s.mu.Lock()
	for _, request := range requests {
		body, err := json.Marshal(request)
		key := "ws " + string(body)
		if err != nil || !wsBatchItem(request) || checkReadOnlyRequest("ws", request) != nil || s.memo[key] != nil || s.replay != nil {
			continue
		}
		m := &relayMemo{done: make(chan struct{})}
		s.memo[key] = m
		items, memos = append(items, body), append(memos, m)
	}
	s.mu.Unlock()
	var slots chan struct{} // nil: every batch at once
	if inFlight > 0 {
		slots = make(chan struct{}, inFlight)
	}
	for start := 0; start < len(items); start += wsBatchMax {
		end := min(start+wsBatchMax, len(items))
		go func() {
			if slots != nil {
				slots <- struct{}{}
				defer func() { <-slots }()
			}
			s.sendBatch(items[start:end], memos[start:end])
		}()
	}
}

// sendBatch sends one batch through the session's request slots, once (a
// resend could repeat reads still under way), and settles every memo: an
// item's own answer, or the batch's failure as each item's failed read.
func (s *relaySession) sendBatch(items []json.RawMessage, memos []*relayMemo) {
	var answer struct {
		Results []struct {
			OK    *bool           `json:"ok"`
			Data  json.RawMessage `json:"data"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"results"`
	}
	body, err := json.Marshal(map[string]any{"batch": items})
	if err == nil {
		s.mu.Lock()
		parent := s.ctx
		s.mu.Unlock()
		var data json.RawMessage
		if data, err = s.exchange(parent, "ws", body, relaySessionRequestTimeout, false); err != nil {
			err = fmt.Errorf("batched read failed: %s", err) // never an item's own not_found
		} else if json.Unmarshal(data, &answer) != nil || len(answer.Results) != len(items) {
			err = fmt.Errorf("Relay ws batch answer does not hold %d results", len(items))
		}
	}
	for _, result := range answer.Results {
		if err == nil && (result.OK == nil || *result.OK && result.Data == nil) {
			err = errors.New("Relay ws batch answer holds a result that is not a result envelope")
		}
	}
	for i, m := range memos {
		switch {
		case err != nil:
			m.err = err
		case *answer.Results[i].OK:
			m.data = answer.Results[i].Data
		default: // as POST /ws answers this read alone: 502 with Home Assistant's own code and message
			r := answer.Results[i].Error
			m.err = &relayCallError{endpoint: "ws", status: http.StatusBadGateway, code: r.Code, message: r.Message}
		}
		close(m.done)
	}
}
