package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	historyUsage     = "ha-nova history <entity_id> [--since 24h|<RFC3339>] [--until <duration ago>|<RFC3339>] [--logbook] (durations ago and the window: at most 7d)"
	historyMaxWindow = 7 * 24 * time.Hour
	historyMaxRows   = 200
)

var verbNow = time.Now

type historyRow struct {
	State string `json:"state"`
	At    string `json:"at"`
}

type historySummary struct {
	Total    int          `json:"total"`
	Returned int          `json:"returned"`
	Changes  int          `json:"changes"`
	First    *historyRow  `json:"first,omitempty"`
	Last     *historyRow  `json:"last,omitempty"`
	Rows     []historyRow `json:"rows"`
}

type logbookSummary struct {
	Total    int               `json:"total"`
	Returned int               `json:"returned"`
	Rows     []json.RawMessage `json:"rows"`
}

type historyResult struct {
	Since   string          `json:"since"`
	Until   string          `json:"until"`
	History historySummary  `json:"history"`
	Logbook *logbookSummary `json:"logbook,omitempty"`
	Note    string          `json:"note,omitempty"`
}

func runHistoryCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	sinceRaw := fs.String("since", "", "window start: a duration ago (90m, 24h, 7d) or an RFC3339 time (default 24h before --until)")
	untilRaw := fs.String("until", "", "window end: a duration ago or an RFC3339 time (default now); at most 7d after --since")
	withLogbook := fs.Bool("logbook", false, "also read the logbook for the same window")
	positional, err := parseVerbArgs(fs, &opts, args, historyUsage)
	entityID := ""
	if err == nil {
		entityID, err = singleEntityID(positional, historyUsage)
	}
	var since, until time.Time
	if err == nil {
		since, until, err = historyBounds(*sinceRaw, *untilRaw, verbNow().UTC().Truncate(time.Second))
	}
	if err != nil {
		return verbUsageError("history", err)
	}
	return runReadVerb(paths, "history", opts, func(s *relaySession, env *verbEnvelope) error {
		_, target, err := readEntityState(s, entityID)
		if err != nil {
			return err
		}
		env.Resolved = []verbTarget{target}
		result := historyResult{Since: since.Format(time.RFC3339), Until: until.Format(time.RFC3339)}
		query := url.Values{"filter_entity_id": {entityID}, "end_time": {result.Until}}.Encode()
		body, err := readCoreOK(s, "/api/history/period/"+result.Since+"?"+query+"&minimal_response&no_attributes")
		if err != nil {
			return err
		}
		var series [][]struct {
			State       string `json:"state"`
			LastChanged string `json:"last_changed"`
		}
		if err := json.Unmarshal(body, &series); err != nil {
			return fmt.Errorf("cannot parse the history of %s: %w", entityID, err)
		}
		rows := []historyRow{}
		if len(series) > 0 {
			for _, point := range series[0] {
				rows = append(rows, historyRow{State: point.State, At: point.LastChanged})
			}
		}
		result.History = summarizeHistory(rows)
		if result.History.Total == 0 && (*sinceRaw != "" || *untilRaw != "") {
			result.Note = "no recorded data in this window (it may be older than the recorder keeps)"
		}
		if *withLogbook {
			body, err := readCoreOK(s, "/api/logbook/"+result.Since+"?"+url.Values{"entity": {entityID}, "end_time": {result.Until}}.Encode())
			if err != nil {
				return err
			}
			var entries []json.RawMessage
			if err := json.Unmarshal(body, &entries); err != nil {
				return fmt.Errorf("cannot parse the logbook of %s: %w", entityID, err)
			}
			kept := lastRows(entries)
			result.Logbook = &logbookSummary{Total: len(entries), Returned: len(kept), Rows: kept}
		}
		env.Result = result
		return nil
	})
}

// historyBounds resolves --since/--until: each a duration ago (90m, 24h,
// 7d) or an RFC3339 time; until defaults to now, since to 24h before until,
// and the window is at most 7 days and never ends in the future — an
// incident window like "since t-30m until t+30m" works too.
func historyBounds(sinceRaw, untilRaw string, now time.Time) (time.Time, time.Time, error) {
	point := func(raw, flag string) (time.Time, error) {
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(raw)); err == nil {
			return at.UTC(), nil
		}
		ago, err := parseHistoryWindow(raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s must be a duration ago of at most 7d (90m, 24h, 7d) or an RFC3339 time", flag)
		}
		return now.Add(-ago), nil
	}
	until, err := now, error(nil)
	if strings.TrimSpace(untilRaw) != "" {
		until, err = point(untilRaw, "--until")
	}
	since := until.Add(-24 * time.Hour)
	if err == nil && strings.TrimSpace(sinceRaw) != "" {
		since, err = point(sinceRaw, "--since")
	}
	switch {
	case err != nil:
		return since, until, err
	case until.After(now):
		return since, until, errors.New("--until must not be in the future")
	case !until.After(since):
		return since, until, errors.New("--until must be later than --since")
	case until.Sub(since) > historyMaxWindow:
		return since, until, errors.New("the window from --since to --until is at most 7d")
	}
	return since, until, nil
}

// parseHistoryWindow accepts Go durations plus whole days ("7d").
func parseHistoryWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	var window time.Duration
	var err error
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		var n int
		n, err = strconv.Atoi(days)
		window = time.Duration(n) * 24 * time.Hour
	} else {
		window, err = time.ParseDuration(raw)
	}
	if err != nil || window <= 0 || window > historyMaxWindow {
		return 0, errors.New("not a positive duration of at most 7d")
	}
	return window, nil
}

// summarizeHistory counts real state changes over all rows and keeps the
// most recent historyMaxRows rows.
func summarizeHistory(rows []historyRow) historySummary {
	summary := historySummary{Total: len(rows), Rows: lastRows(rows)}
	summary.Returned = len(summary.Rows)
	for i := range rows {
		if i > 0 && rows[i].State != rows[i-1].State {
			summary.Changes++
		}
	}
	if len(rows) > 0 {
		summary.First, summary.Last = &rows[0], &rows[len(rows)-1]
	}
	return summary
}

func lastRows[T any](rows []T) []T {
	if len(rows) > historyMaxRows {
		return rows[len(rows)-historyMaxRows:]
	}
	return rows
}

func readCoreOK(s *relaySession, path string) (json.RawMessage, error) {
	status, body, err := s.coreGET(path)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("Home Assistant answered HTTP %d for %s", status, path)
	}
	return body, nil
}
