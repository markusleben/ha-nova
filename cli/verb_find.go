package main

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const findUsage = `ha-nova find ["<phrase>"] [--domain <d>] [--area <area_id>] [--state <state>] [--limit 20] [--include-disabled] [--aliases]` +
	"\n(the phrase may be left out when --domain, --area, or --state narrows the list)"

type findQuery struct {
	phrase, domain, area string
	state                string // only entities currently in this state
	limit                int
	includeDisabled      bool
	// completeAliases: uniqueness checks (plan/call --match) always run the
	// alias pass, so a name match never hides an entity that carries the
	// phrase as an alias. find itself only lists and keeps the cheap path.
	completeAliases bool
}

type findResult struct {
	Total    int          `json:"total"`
	Returned int          `json:"returned"`
	Matches  []verbTarget `json:"matches"`
	// unknownDomain: no registry row and no state has the --domain prefix
	// (a typo like "lights"), so an empty listing is no answer.
	unknownDomain bool
	// statesUnread: /api/states failed, so a phrase search saw only the
	// registry (no unregistered entities, no friendly names).
	statesUnread error
}

// registryRow is one entity from either registry listing, already joined
// with its effective area (entity area, else device area).
type registryRow struct {
	entityID, name, areaID, area string
	aliases                      []string
	disabled                     bool
}

func runFindCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	query := findQuery{}
	fs := flag.NewFlagSet("find", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	fs.StringVar(&query.domain, "domain", "", "only entities of this domain")
	fs.StringVar(&query.area, "area", "", "only entities in this area_id (entity or device area)")
	fs.IntVar(&query.limit, "limit", 20, "maximum rows returned (1-200); total stays exact")
	fs.BoolVar(&query.includeDisabled, "include-disabled", false, "include disabled entities (reads the full registry)")
	fs.BoolVar(&query.completeAliases, "aliases", false, "also match aliases even when names already match (use after a rejected name match)")
	fs.StringVar(&query.state, "state", "", "only entities whose current state is exactly this (e.g. on, off, unavailable)")
	positional, err := parseVerbArgs(fs, &opts, args, findUsage)
	query.state = strings.TrimSpace(query.state)
	filtered := query.domain != "" || query.area != "" || query.state != ""
	switch {
	case err != nil:
	case len(positional) > 1, len(positional) == 1 && len(foldTokens(positional[0])) == 0, len(positional) == 0 && !filtered:
		err = errors.New("Usage: " + findUsage)
	case len(positional) == 1:
		query.phrase = positional[0]
	}
	if err == nil && (query.limit < 1 || query.limit > 200) {
		err = errors.New("--limit must be between 1 and 200")
	}
	if err != nil {
		return verbUsageError("find", err)
	}
	return runReadVerb(paths, "find", opts, func(s *relaySession, env *verbEnvelope) error {
		result, err := findEntities(s, query)
		if err != nil {
			return err
		}
		if result.statesUnread != nil {
			if result.Total == 0 { // no hit proves absence only with every state read
				return result.statesUnread
			}
			env.Notices = append(env.Notices, statesUnreadNotice(result.statesUnread))
		}
		// A filter-only listing with no hits is an answer ("no light is on"),
		// not a failed lookup — unless its domain exists nowhere; a phrase
		// that matches nothing is NOT_FOUND.
		if result.unknownDomain {
			return &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("no entity has domain %q", query.domain),
				next: "Check the domain: it is the part before the dot of an entity_id (light, switch, sensor, …)."}
		}
		if result.Total == 0 && query.phrase != "" {
			next := "Names may be in another language than the user's: list that kind (--domain light) or the area before you say none exists. Or try a shorter or different phrase, drop --domain/--area, or add --include-disabled."
			if query.state != "" {
				next = "Drop --state to see the matching entities and their current state."
			}
			return &verbFailure{
				exit: verbExitNotFound, code: "NOT_FOUND",
				message: fmt.Sprintf("no entity matches %q%s", query.phrase, stateSuffix(query.state)), next: next,
			}
		}
		env.Result = result
		return nil
	})
}

// findEntities matches the compact registry first; the full registry is read
// only when that finds nothing or disabled rows are wanted, and aliases
// (config/entity_registry/get_entries, the lists carry none) only when names
// miss there too. One /api/states read (session memo) adds each entity's
// friendly_name to what registry rows match on, and the entities with a state
// but no registry row (YAML, template, demo) in mergeStates. A phrase search
// survives a failed states read on the registry alone (statesUnread); a
// listing (--state, or no phrase) cannot.
func findEntities(s *relaySession, q findQuery) (findResult, error) {
	areas, deviceAreas, err := loadAreaContext(s)
	if err != nil {
		return findResult{}, err
	}
	states, statesErr := loadStates(s)
	if statesErr != nil && (q.state != "" || q.phrase == "") {
		return findResult{}, statesErr
	}
	friendly := map[string]string{}
	for _, st := range states {
		friendly[st.EntityID] = strings.TrimSpace(st.Attributes.FriendlyName)
	}
	if q.area != "" {
		if _, ok := areas[q.area]; !ok {
			return findResult{}, &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("unknown area_id %q", q.area), next: "Pass a canonical area_id from the area registry."}
		}
	}
	var matches, hits []verbTarget
	if statesErr == nil {
		if hits, err = stateHits(s, states, q); err != nil {
			return findResult{}, err
		}
	}
	domainKnown := q.domain == ""
	hasDomain := func(rows []registryRow) bool {
		return slices.ContainsFunc(rows, func(row registryRow) bool { return strings.HasPrefix(row.entityID, q.domain+".") })
	}
	if !q.includeDisabled {
		rows, err := loadCompactRegistry(s, areas, deviceAreas)
		if err != nil {
			return findResult{}, err
		}
		matches, domainKnown = matchRegistryRows(rows, q, friendly), domainKnown || hasDomain(rows)
	}
	if len(matches) == 0 || q.completeAliases {
		rows, err := loadFullRegistry(s, areas, deviceAreas)
		if err != nil {
			return findResult{}, err
		}
		domainKnown = domainKnown || hasDomain(rows)
		full := matchRegistryRows(rows, q, friendly)
		if (len(full) == 0 && len(hits) == 0) || q.completeAliases { // a state-name hit is a name hit too
			if err := attachAliases(s, rows, q); err != nil {
				return findResult{}, err
			}
			full = matchRegistryRows(rows, q, friendly)
		}
		matches = unionTargets(matches, full)
	}
	if statesErr == nil {
		var seen bool
		matches, seen = mergeStates(states, friendly, matches, hits, q)
		domainKnown = domainKnown || seen
	}
	if len(matches) == 0 && q.state != "" && q.phrase != "" && !q.completeAliases {
		// the names matched only entities in other states: an alias may
		// name one in this state, as it would without --state
		q.completeAliases = true
		return findEntities(s, q)
	}
	result := findResult{Total: len(matches), Matches: matches, unknownDomain: len(matches) == 0 && !domainKnown, statesUnread: statesErr}
	if len(matches) > q.limit {
		result.Matches = matches[:q.limit]
	}
	result.Returned = len(result.Matches)
	return result, nil
}

func stateSuffix(state string) string {
	if state == "" {
		return ""
	}
	return " in state " + strconv.Quote(state)
}

// unionTargets keeps every entity once (first basis wins), sorted by id.
func unionTargets(a, b []verbTarget) []verbTarget {
	seen := map[string]bool{}
	out := []verbTarget{}
	for _, t := range append(append([]verbTarget{}, a...), b...) {
		if !seen[t.EntityID] {
			seen[t.EntityID] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EntityID < out[j].EntityID })
	return out
}

func rowInScope(row registryRow, q findQuery) bool {
	return (!row.disabled || q.includeDisabled) &&
		(q.domain == "" || strings.HasPrefix(row.entityID, q.domain+".")) &&
		(q.area == "" || row.areaID == q.area)
}

// aliasChunk bounds one get_entries request; chunks run in parallel within
// the session's request slots.
const aliasChunk = 500

// attachAliases reads aliases for the in-scope rows only.
func attachAliases(s *relaySession, rows []registryRow, q findQuery) error {
	index := map[string]int{}
	var ids []string
	for i, row := range rows {
		if rowInScope(row, q) {
			index[row.entityID] = i
			ids = append(ids, row.entityID)
		}
	}
	var requests []map[string]any
	for start := 0; start < len(ids); start += aliasChunk {
		requests = append(requests, map[string]any{"type": "config/entity_registry/get_entries", "entity_ids": ids[start:min(start+aliasChunk, len(ids))]})
	}
	var reads []func()
	for _, request := range requests {
		reads = append(reads, func() { _, _ = s.ws(request) })
	}
	prefetch(reads)
	for _, request := range requests {
		var entries map[string]*struct {
			Aliases []*string `json:"aliases"`
		}
		if err := s.wsDecode(request, &entries, "the entity aliases"); err != nil {
			return err
		}
		for id, entry := range entries {
			if i, ok := index[id]; ok && entry != nil {
				for _, alias := range entry.Aliases {
					if alias != nil {
						rows[i].aliases = append(rows[i].aliases, *alias)
					}
				}
			}
		}
	}
	return nil
}

// matchRegistryRows matches entity_id, registry name, friendly_name (a
// device-named entity has no registry name), and area; aliases after them.
func matchRegistryRows(rows []registryRow, q findQuery, friendly map[string]string) []verbTarget {
	tokens := foldTokens(q.phrase)
	matches := []verbTarget{}
	for _, row := range rows {
		if !rowInScope(row, q) {
			continue
		}
		by := "name"
		text := row.entityID + " " + row.name + " " + friendly[row.entityID] + " " + row.area
		if !tokensMatch(tokens, text) {
			if len(row.aliases) == 0 || !tokensMatch(tokens, text+" "+strings.Join(row.aliases, " ")) {
				continue
			}
			by = "alias"
		}
		matches = append(matches, verbTarget{EntityID: row.entityID, Name: row.name, AreaID: row.areaID, Area: row.area, Disabled: row.disabled, By: by})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].EntityID < matches[j].EntityID })
	return matches
}

func loadAreaContext(s *relaySession) (map[string]string, map[string]string, error) {
	var areaRows []struct {
		AreaID string `json:"area_id"`
		Name   string `json:"name"`
	}
	var deviceRows []struct {
		ID     string `json:"id"`
		AreaID string `json:"area_id"`
		Parent string `json:"parent_device_id"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/area_registry/list"}, &areaRows, "the area registry"); err != nil {
		return nil, nil, err
	}
	if err := s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &deviceRows, "the device registry"); err != nil {
		return nil, nil, err
	}
	areas := map[string]string{}
	for _, a := range areaRows {
		areas[a.AreaID] = a.Name
	}
	own, parent := map[string]string{}, map[string]string{}
	for _, d := range deviceRows {
		own[d.ID], parent[d.ID] = d.AreaID, d.Parent
	}
	return areas, effectiveDeviceAreas(own, parent), nil
}

// effectiveDeviceAreas: a device's own area_id; a child device (Home
// Assistant 2026.9+ parent_device_id) without one inherits its parent's
// effective area, however deep. A parent missing from the read, or a cycle,
// leaves it unknown.
func effectiveDeviceAreas(own, parent map[string]string) map[string]string {
	out := map[string]string{}
	for id := range own {
		cur, seen := id, map[string]bool{}
		for own[cur] == "" && parent[cur] != "" && !seen[cur] {
			seen[cur], cur = true, parent[cur]
		}
		out[id] = own[cur]
	}
	return out
}

func loadCompactRegistry(s *relaySession, areas, deviceAreas map[string]string) ([]registryRow, error) {
	var compact struct {
		Entities []struct {
			EI, EN, AI, DI string
		} `json:"entities"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list_for_display"}, &compact, "the compact entity registry"); err != nil {
		return nil, err
	}
	rows := make([]registryRow, 0, len(compact.Entities))
	for _, e := range compact.Entities {
		rows = append(rows, joinArea(registryRow{entityID: e.EI, name: e.EN, areaID: e.AI}, e.DI, areas, deviceAreas))
	}
	return rows, nil
}

func loadFullRegistry(s *relaySession, areas, deviceAreas map[string]string) ([]registryRow, error) {
	var full []struct {
		EntityID     string `json:"entity_id"`
		Name         string `json:"name"`
		OriginalName string `json:"original_name"`
		AreaID       string `json:"area_id"`
		DeviceID     string `json:"device_id"`
		DisabledBy   string `json:"disabled_by"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &full, "the entity registry"); err != nil {
		return nil, err
	}
	rows := make([]registryRow, 0, len(full))
	for _, e := range full {
		row := registryRow{entityID: e.EntityID, name: e.Name, areaID: e.AreaID, disabled: e.DisabledBy != ""}
		if row.name == "" {
			row.name = e.OriginalName
		}
		rows = append(rows, joinArea(row, e.DeviceID, areas, deviceAreas))
	}
	return rows, nil
}

func joinArea(row registryRow, deviceID string, areas, deviceAreas map[string]string) registryRow {
	if row.areaID == "" {
		row.areaID = deviceAreas[deviceID]
	}
	row.area = areas[row.areaID]
	return row
}

// foldTokens splits a phrase into lowercase letter/digit tokens.
func foldTokens(phrase string) []string {
	return strings.FieldsFunc(strings.ToLower(stripCombiningMarks(phrase)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// tokensMatch reports whether every token occurs in the haystack, comparing
// case- and diacritic-folded: "Küche" matches "kuche" and "kueche" and back.
func tokensMatch(tokens []string, haystack string) bool {
	lower := strings.ToLower(stripCombiningMarks(haystack))
	hay := [2]string{diacriticStrip.Replace(lower), germanTransliterate.Replace(lower)}
	for _, token := range tokens {
		variants := [2]string{diacriticStrip.Replace(token), germanTransliterate.Replace(token)}
		if !strings.Contains(hay[0], variants[0]) && !strings.Contains(hay[0], variants[1]) &&
			!strings.Contains(hay[1], variants[0]) && !strings.Contains(hay[1], variants[1]) {
			return false
		}
	}
	return true
}

// stripCombiningMarks drops decomposed accents ("e\u0301" → "e") so that
// canonically equivalent text folds like its precomposed form.
func stripCombiningMarks(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, text)
}

// ponytail: fixed table of common Latin diacritics (NFD strip by hand, no
// x/text dependency); add golang.org/x/text/unicode/norm if non-Latin-1/A
// scripts show up in real registries.
var diacriticStrip = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ą", "a", "æ", "ae",
	"ç", "c", "ć", "c", "č", "c", "ď", "d", "đ", "d",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ę", "e", "ě", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ı", "i", "ğ", "g", "ł", "l",
	"ñ", "n", "ń", "n", "ň", "n", "ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "œ", "oe",
	"ř", "r", "ś", "s", "š", "s", "ş", "s", "ß", "ss", "ť", "t",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ů", "u", "ý", "y", "ÿ", "y", "ź", "z", "ż", "z", "ž", "z",
)

var germanTransliterate = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
