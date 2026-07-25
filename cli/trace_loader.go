package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

func loadLatestTrace(paths runtimePaths, entityID, domain string) (traceLatestOutput, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	return loadLatestTraceWithContext(ctx, paths, entityID, domain)
}

func loadLatestTraceWithContext(
	ctx context.Context,
	paths runtimePaths,
	entityID, domain string,
) (traceLatestOutput, error) {
	listOut, err := loadTraceListWithContext(ctx, paths, entityID, domain)
	if err != nil {
		return traceLatestOutput{}, err
	}
	if len(listOut.Traces) == 0 {
		return traceLatestOutput{}, fmt.Errorf("no traces found for %s; Home Assistant keeps only recent traces, and YAML automations/scripts need an id to be traceable", entityID)
	}
	latest := listOut.Traces[0]
	getOut, err := loadTraceGetWithUniqueIDContext(
		ctx,
		paths,
		entityID,
		domain,
		listOut.UniqueID,
		latest.RunID,
	)
	if err != nil {
		return traceLatestOutput{}, err
	}
	return traceLatestOutput{
		SchemaVersion: 1,
		OK:            true,
		EntityID:      entityID,
		Domain:        domain,
		UniqueID:      listOut.UniqueID,
		RunID:         latest.RunID,
		Timestamp:     latest.Timestamp,
		LastStep:      getOut.LastStep,
		Error:         getOut.Error,
		Trace:         getOut.Trace,
	}, nil
}

func loadTraceList(paths runtimePaths, entityID, domain string) (traceListOutput, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	return loadTraceListWithContext(ctx, paths, entityID, domain)
}

func loadTraceListWithContext(
	ctx context.Context,
	paths runtimePaths,
	entityID, domain string,
) (traceListOutput, error) {
	uniqueID, err := resolveTraceUniqueIDContext(ctx, paths, entityID)
	if err != nil {
		return traceListOutput{}, err
	}
	listBody, err := relayWSJSONContext(ctx, paths, map[string]string{
		"type":    "trace/list",
		"domain":  domain,
		"item_id": uniqueID,
	})
	if err != nil {
		return traceListOutput{}, err
	}
	entries, err := parseTraceList(listBody)
	if err != nil {
		return traceListOutput{}, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return traceTimestampAfter(entries[i].Timestamp, entries[j].Timestamp)
	})
	return traceListOutput{
		SchemaVersion: 1,
		OK:            true,
		EntityID:      entityID,
		Domain:        domain,
		UniqueID:      uniqueID,
		Count:         len(entries),
		Traces:        entries,
	}, nil
}

func loadTraceGet(paths runtimePaths, entityID, domain, runID string) (traceGetOutput, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	uniqueID, err := resolveTraceUniqueIDContext(ctx, paths, entityID)
	if err != nil {
		return traceGetOutput{}, err
	}
	return loadTraceGetWithUniqueIDContext(
		ctx,
		paths,
		entityID,
		domain,
		uniqueID,
		runID,
	)
}

func loadTraceGetWithUniqueIDContext(
	ctx context.Context,
	paths runtimePaths,
	entityID, domain, uniqueID, runID string,
) (traceGetOutput, error) {
	if strings.TrimSpace(runID) == "" {
		return traceGetOutput{}, fmt.Errorf("trace get requires a run_id from trace list")
	}
	traceBody, err := relayWSJSONContext(ctx, paths, map[string]string{
		"type":    "trace/get",
		"domain":  domain,
		"item_id": uniqueID,
		"run_id":  runID,
	})
	if err != nil {
		return traceGetOutput{}, err
	}
	summary := traceGetSummary(traceBody)
	return traceGetOutput{
		SchemaVersion:   1,
		OK:              true,
		EntityID:        entityID,
		Domain:          domain,
		UniqueID:        uniqueID,
		RunID:           runID,
		ItemID:          summary.ItemID,
		Timestamp:       summary.Timestamp,
		LastStep:        summary.LastStep,
		State:           summary.State,
		ScriptExecution: summary.ScriptExecution,
		Error:           summary.Error,
		Trace:           extractRelayData(traceBody),
	}, nil
}

func resolveTraceUniqueIDContext(
	ctx context.Context,
	paths runtimePaths,
	entityID string,
) (string, error) {
	body, err := relayWSJSONContext(ctx, paths, map[string]string{
		"type":      "config/entity_registry/get",
		"entity_id": entityID,
	})
	if err != nil {
		return "", err
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			UniqueID string `json:"unique_id"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("cannot parse entity registry response: %w", err)
	}
	if !envelope.OK {
		if envelope.Error.Message != "" {
			return "", fmt.Errorf("cannot resolve %s: %s", entityID, envelope.Error.Message)
		}
		return "", fmt.Errorf("cannot resolve %s", entityID)
	}
	if strings.TrimSpace(envelope.Data.UniqueID) == "" {
		return "", fmt.Errorf("cannot resolve %s: entity registry entry has no unique_id", entityID)
	}
	return envelope.Data.UniqueID, nil
}

func relayWSJSONContext(
	ctx context.Context,
	paths runtimePaths,
	payload any,
) ([]byte, error) {
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	// Route through the paired device transport when this install is paired,
	// falling back to the legacy token — the same resolution the other functional
	// commands use — so `trace` works on passwordless installs and fails closed
	// when a paired credential is missing instead of using the wrong transport.
	selected, err := selectRelayTransport(ctx, cfg, "", false)
	if err != nil {
		return nil, fmt.Errorf("%s", relayTransportErrorMessage(err))
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(selected.BaseURL, "/") + "/ws"
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+selected.Credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := selected.Client.Do(req)
	if err != nil {
		return nil, relayRequestOutcomeUnknownError(selected.BaseURL, err)
	}
	defer resp.Body.Close()
	return readRelayWSJSONResponse(resp, maxRelayResponseBytes)
}

func readRelayWSJSONResponse(
	response *http.Response,
	maxBytes int64,
) ([]byte, error) {
	if isHTTPRedirect(response.StatusCode) ||
		response.StatusCode >= http.StatusInternalServerError {
		return nil, relayHTTPOutcomeUnknownError(response.StatusCode)
	}
	body, err := readAllLimited(response.Body, maxBytes)
	if err != nil {
		return nil, relayPostRequestOutcomeUnknownError(
			"reading the Relay trace response",
			err,
		)
	}
	body, err = normalizeUTF8Bytes(body, "Relay trace response")
	if err != nil {
		return nil, relayPostRequestOutcomeUnknownError(
			"validating the Relay trace response",
			err,
		)
	}
	var envelope struct {
		OK    *bool `json:"ok"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.OK == nil {
		if err == nil {
			err = errors.New("Relay trace response did not contain a valid result envelope")
		}
		return nil, relayPostRequestOutcomeUnknownError(
			"validating the Relay trace result envelope",
			err,
		)
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("relay ws failed with HTTP %d", response.StatusCode)
	}
	if !*envelope.OK {
		if envelope.Error.Message != "" {
			return nil, fmt.Errorf("relay ws failed: %s", envelope.Error.Message)
		}
		return nil, fmt.Errorf("relay ws failed")
	}
	return body, nil
}

func relayWSJSON(paths runtimePaths, payload any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	return relayWSJSONContext(ctx, paths, payload)
}
