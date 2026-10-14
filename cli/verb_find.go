package main

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const findUsage = `ha-nova find "<phrase>" [--domain <d>] [--area <area_id>] [--limit 20] [--include-disabled]`

type findQuery struct {
	phrase, domain, area string
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
	positional, err := parseVerbArgs(fs, &opts, args, findUsage)
	if err == nil && (len(positional) != 1 || len(foldTokens(positional[0])) == 0) {
		err = errors.New("Usage: " + findUsage)
	}
	if err == nil && (query.limit < 1 || query.limit > 200) {
		err = errors.New("--limit must be between 1 and 200")
	}
	if err != nil {
		return verbUsageError("find", err)
	}
	query.phrase = positional[0]
	return runReadVerb(paths, "find", opts, func(s *relaySession, env *verbEnvelope) error {
		result, err := findEntities(s, query)
		if err != nil {
			return err
		}
		if result.Total == 0 {
			return &verbFailure{
				exit: verbExitNotFound, code: "NOT_FOUND",
				message: fmt.Sprintf("no entity matches %q", query.phrase),
				next:    "Try a shorter or different phrase, drop --domain/--area, or add --include-disabled.",
			}
		}
		env.Result = result
		return nil
	})
}

// findEntities matches the compact registry first; the full registry is read
// only when that finds nothing or disabled rows are wanted, and aliases
// (config/entity_registry/get_entries, the lists carry none) only when names
// miss there too.
func findEntities(s *relaySession, q findQuery) (findResult, error) {
	areas, deviceAreas, err := loadAreaContext(s)
	if err != nil {
		return findResult{}, err
	}
	if q.area != "" {
		if _, ok := areas[q.area]; !ok {
			return findResult{}, &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("unknown area_id %q", q.area), next: "Pass a canonical area_id from the area registry."}
		}
	}
	var matches []verbTarget
	if !q.includeDisabled {
		rows, err := loadCompactRegistry(s, areas, deviceAreas)
		if err != nil {
			return findResult{}, err
		}
		matches = matchRegistryRows(rows, q)
	}
	if len(matches) == 0 || q.completeAliases {
		rows, err := loadFullRegistry(s, areas, deviceAreas)
		if err != nil {
			return findResult{}, err
		}
		full := matchRegistryRows(rows, q)
		if len(full) == 0 || q.completeAliases {
			if err := attachAliases(s, rows, q); err != nil {
				return findResult{}, err
			}
			full = matchRegistryRows(rows, q)
		}
		matches = unionTargets(matches, full)
	}
	result := findResult{Total: len(matches), Matches: matches}
	if len(matches) > q.limit {
		result.Matches = matches[:q.limit]
	}
	result.Returned = len(result.Matches)
	return result, nil
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

func matchRegistryRows(rows []registryRow, q findQuery) []verbTarget {
	tokens := foldTokens(q.phrase)
	matches := []verbTarget{}
	for _, row := range rows {
		if !rowInScope(row, q) {
			continue
		}
		by := "name"
		if !tokensMatch(tokens, row.entityID+" "+row.name+" "+row.area) {
			if len(row.aliases) == 0 || !tokensMatch(tokens, row.entityID+" "+row.name+" "+row.area+" "+strings.Join(row.aliases, " ")) {
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
	deviceAreas := map[string]string{}
	for _, d := range deviceRows {
		deviceAreas[d.ID] = d.AreaID
	}
	return areas, deviceAreas, nil
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
