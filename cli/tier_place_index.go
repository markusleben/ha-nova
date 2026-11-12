package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Place-trigger index (MAR-149, docs/work/2026-11-12-place-trigger-index-spec.md).
// Home Assistant relates to an area or floor every automation of its devices
// and entities; one found only there is a consumer only when a trigger
// targets the place (targetsPlace). The index keeps the trigger targets of
// such an automation under the last_changed of its on or off state: Home
// Assistant keeps an automation entity only while its name and raw config
// are the same (automation/util.py, automation_matches_config), and a
// replaced one passes through unavailable or loses its state, so an on or
// off state with the same last_changed has the same triggers. The index only
// ever skips a read; everything it can't vouch for is read live, as before.

const (
	placeIndexVersion    = 1
	placeIndexMaxEntries = tierMaxAutomations
	placeIndexMaxBytes   = 4 << 20
)

type placeIndexEntry struct {
	Stamp    string `json:"stamp"`
	Triggers []any  `json:"triggers"` // {"target": …} of each enabled trigger that has one; null: no proof
}

type placeIndexFile struct {
	Version int                        `json:"version"`
	Scope   string                     `json:"scope"`
	Entries map[string]placeIndexEntry `json:"entries"`
}

type placeIndex struct {
	mu      sync.Mutex
	path    string
	scope   string // "": this session's home can't be told apart, no index
	entries map[string]placeIndexEntry
	changed map[string]placeIndexEntry
}

// placeIndex loads the session's index once. A file of another home,
// version, or shape, or one too large, is an empty index.
func (s *relaySession) placeIndex() *placeIndex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.placeIdx == nil {
		x := &placeIndex{path: filepath.Join(s.paths.CacheDir, "place-index.json"), changed: map[string]placeIndexEntry{}}
		if s.cfg.RelayBaseURL != "" { // the profile and every id of the Relay and home it is paired with
			sum := sha256.Sum256([]byte(fmt.Sprintf("%q", []string{s.cfg.ProfileID, s.cfg.RelayInstanceID, s.cfg.RelaySpkiPin, s.cfg.RelayBaseURL, s.cfg.HAURL})))
			x.scope = hex.EncodeToString(sum[:])
		}
		x.entries = x.read()
		s.placeIdx = x
	}
	return s.placeIdx
}

func (x *placeIndex) read() map[string]placeIndexEntry {
	var file placeIndexFile
	if info, err := os.Stat(x.path); err != nil || info.Size() > placeIndexMaxBytes {
		return map[string]placeIndexEntry{}
	}
	if data, err := os.ReadFile(x.path); err != nil || json.Unmarshal(data, &file) != nil ||
		x.scope == "" || file.Version != placeIndexVersion || file.Scope != x.scope || file.Entries == nil {
		return map[string]placeIndexEntry{}
	}
	return file.Entries
}

// save writes the file with this session's entries over its current ones,
// through a unique temp file and a rename; a failure costs reads only.
func (x *placeIndex) save() {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.changed) == 0 || x.scope == "" {
		return
	}
	entries := x.read()
	if len(entries)+len(x.changed) > placeIndexMaxEntries {
		entries = map[string]placeIndexEntry{}
	}
	for automation, entry := range x.changed {
		entries[automation] = entry
	}
	data, err := json.Marshal(placeIndexFile{Version: placeIndexVersion, Scope: x.scope, Entries: entries})
	if err != nil || len(data) > placeIndexMaxBytes || os.MkdirAll(filepath.Dir(x.path), 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(x.path), ".place-index-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err != nil || closeErr != nil || os.Rename(tmp.Name(), x.path) != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	x.changed = map[string]placeIndexEntry{}
}

// placeOnlyMiss: whether an automation found only where an entity sits
// certainly has no trigger that targets one of places, so it is no consumer.
// From the index when its stamp is current and it targets none of them;
// else from its config (a hit that targets one is read too, for its scan);
// false when that config can't be read (it goes on as a consumer: typed,
// never clean). Safe to call concurrently (session reads only).
func (e *tierEngine) placeOnlyMiss(automation string, places [][2]string) bool {
	stamp := e.stateStamp(automation)
	key := fmt.Sprintf("place-index %s %s %v", automation, stamp, places) // a cut preview's skip, for its replay (plan_replay.go)
	if e.s.replaying() {
		if made, known := e.s.replayed(key); known && made && stamp != "" {
			return true
		}
		return e.liveMiss(automation, places)
	}
	config := map[string]any{"type": "automation/config", "entity_id": automation}
	if stamp == "" || e.s.memoized("ws", config) { // read already (maybe before this state): that read decides, and proves no pair
		return e.liveMiss(automation, places)
	}
	x := e.s.placeIndex()
	x.mu.Lock()
	entry, hit := x.entries[automation]
	x.mu.Unlock()
	if hit && entry.Stamp == stamp && entry.Triggers != nil && !targetsPlace(map[string]any{"triggers": entry.Triggers}, places) {
		return e.s.decide(key, true)
	}
	// The state answered before this config read went out: a reload in
	// between gives the config a stamp that is already stale, never the
	// other way round.
	cfg := e.warmConfig(automation)
	if cfg == nil {
		return false
	}
	if raw, ok, err := loadedConfig(e.s, automation, "automation"); err == nil && ok && !unresolvedBlueprint(raw) && x.scope != "" {
		triggers := []any{}
		for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
			if target, present := trigger["target"]; present {
				triggers = append(triggers, map[string]any{"target": target})
			}
		}
		x.mu.Lock()
		x.entries[automation] = placeIndexEntry{Stamp: stamp, Triggers: triggers}
		x.changed[automation] = x.entries[automation]
		x.mu.Unlock()
	}
	return !targetsPlace(cfg, places)
}

func (e *tierEngine) liveMiss(automation string, places [][2]string) bool {
	cfg := e.warmConfig(automation)
	return cfg != nil && !targetsPlace(cfg, places)
}

// stateStamp: the last_changed of an automation's on or off state; "" for
// any other state (unavailable may survive a replacement), none, or a read
// that fails.
func (e *tierEngine) stateStamp(id string) string {
	body, present, err := e.rawState(id)
	var st struct {
		State       string `json:"state"`
		LastChanged string `json:"last_changed"`
	}
	if err != nil || !present || json.Unmarshal(body, &st) != nil || st.State != "on" && st.State != "off" {
		return ""
	}
	return st.LastChanged
}

// unresolvedBlueprint: Home Assistant answers a blueprint instance already
// substituted; one still naming its blueprint depends on that file.
func unresolvedBlueprint(raw json.RawMessage) bool {
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil {
		return true
	}
	_, uses := cfg["use_blueprint"]
	return uses
}

// memoized: whether this session has sent the read already.
func (s *relaySession) memoized(endpoint string, payload map[string]any) bool {
	body, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, sent := s.memo[endpoint+" "+string(body)]
	return sent
}

// replayed: the preview's choice under key, without marking a replay
// diverged when the preview made none.
func (s *relaySession) replayed(key string) (made, known bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	made, known = s.replay[key]
	return made, known
}
