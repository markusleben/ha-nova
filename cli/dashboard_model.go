package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
)

// Lovelace dashboard structure (dashboard-write spec): views, their card
// containers, readable card names, and the read-only issue checks shared by
// plan write and get --dashboard(s). Nothing here talks to Home Assistant.

type dashboardIssue struct {
	Kind     string `json:"kind"` // missing_entity | card_type_unconfirmed
	Where    string `json:"where"`
	EntityID string `json:"entity_id,omitempty"`
	CardType string `json:"card_type,omitempty"`
	Detail   string `json:"detail"`
	New      bool   `json:"new,omitempty"` // plan: the draft adds this reference
}

type lovelaceResource struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// cardContainer is one list of cards in a view: its cards, a section's
// cards, or its badges.
type cardContainer struct {
	label string // "" (the view's own cards), "section 2", "badges"
	items []any
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func dashboardViews(cfg map[string]any) []map[string]any {
	var views []map[string]any
	for _, v := range asList(cfg["views"]) {
		views = append(views, asMap(v)) // a non-object view stays a nil map, never dropped
	}
	return views
}

func viewLabel(i int, v map[string]any) string {
	if title, _ := v["title"].(string); title != "" {
		return fmt.Sprintf("View %q", title)
	}
	if p, _ := v["path"].(string); p != "" {
		return "View /" + p
	}
	return fmt.Sprintf("View %d", i+1)
}

// viewKeys match views across before/after: path, else title, else the
// position; a repeated key gets its occurrence number.
func viewKeys(views []map[string]any) []string {
	seen := map[string]int{}
	keys := make([]string, len(views))
	for i, v := range views {
		key := fmt.Sprintf("#%d", i)
		if p, _ := v["path"].(string); p != "" {
			key = "path:" + p
		} else if title, _ := v["title"].(string); title != "" {
			key = "title:" + title
		}
		seen[key]++
		keys[i] = fmt.Sprintf("%s#%d", key, seen[key])
	}
	return keys
}

func viewContainers(v map[string]any) []cardContainer {
	containers := []cardContainer{{label: "", items: asList(v["cards"])}}
	for i, section := range asList(v["sections"]) {
		containers = append(containers, cardContainer{label: fmt.Sprintf("section %d", i+1), items: asList(asMap(section)["cards"])})
	}
	return append(containers, cardContainer{label: "badges", items: asList(v["badges"])})
}

// containerKeys are the view keys diffed as card lists, not as values.
var containerKeys = map[string]bool{"cards": true, "sections": true, "badges": true}

func itemLabel(container string, i int) string {
	noun := "card"
	if container == "badges" {
		return fmt.Sprintf("badge %d", i+1)
	}
	if container != "" {
		return fmt.Sprintf("%s › %s %d", container, noun, i+1)
	}
	return fmt.Sprintf("%s %d", noun, i+1)
}

// cardSummary names a card the way a person recognizes it: its type plus
// the entity, heading, or title it shows.
func cardSummary(card any) string {
	m := asMap(card)
	if m == nil {
		return formatValue(card) // a badge may be a bare entity_id
	}
	kind, _ := m["type"].(string)
	if kind == "" {
		kind = "card"
	}
	for _, key := range []string{"entity", "heading", "title", "name"} {
		if text, _ := m[key].(string); text != "" {
			if key == "entity" {
				return kind + " " + text
			}
			return fmt.Sprintf("%s %q", kind, text)
		}
	}
	if entities := asList(m["entities"]); len(entities) > 0 {
		return fmt.Sprintf("%s (%d entities)", kind, len(entities))
	}
	if cards := asList(m["cards"]); len(cards) > 0 {
		return fmt.Sprintf("%s (%d cards)", kind, len(cards))
	}
	return kind
}

// viewSummary: "sections view, 3 sections, 12 cards".
func viewSummary(v map[string]any) string {
	kind, _ := v["type"].(string)
	if kind == "" {
		kind = "masonry"
	}
	parts := []string{kind + " view"}
	if n := len(asList(v["sections"])); n > 0 {
		parts = append(parts, plural(n, "section"))
	}
	return strings.Join(append(parts, plural(viewCardCount(v), "card")), ", ")
}

func viewCardCount(v map[string]any) int {
	n := 0
	for _, c := range viewContainers(v) {
		if c.label != "badges" {
			n += len(c.items)
		}
	}
	return n
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// placedCard is one card (or badge, or a view's own settings) with its
// readable place; refs walks everything inside it.
type placedCard struct {
	where string
	value any
}

func placedCards(cfg map[string]any) []placedCard {
	rest := map[string]any{}
	for k, v := range cfg {
		if k != "views" {
			rest[k] = v
		}
	}
	placed := []placedCard{{where: "Dashboard", value: rest}}
	for i, v := range dashboardViews(cfg) {
		label := viewLabel(i, v)
		own := map[string]any{}
		for k, val := range v {
			if !containerKeys[k] {
				own[k] = val
			}
		}
		for _, section := range asList(v["sections"]) { // section settings (and a heading's) belong to the view
			for k, val := range asMap(section) {
				if k != "cards" {
					own[k+"@section"] = val
				}
			}
		}
		placed = append(placed, placedCard{where: label, value: own})
		for _, c := range viewContainers(v) {
			for j, card := range c.items {
				placed = append(placed, placedCard{where: label + " › " + itemLabel(c.label, j) + " (" + cardSummary(card) + ")", value: card})
			}
		}
	}
	return placed
}

// entityRefKeys hold entity ids in Lovelace configs (cards, rows, badges,
// conditions, action targets); other strings that look like ids (an action
// name such as light.turn_on) are never read as entities.
var entityRefKeys = map[string]bool{"entity": true, "entity_id": true, "entities": true, "camera_image": true}

// dashboardRefs collects the literal entity ids and custom: types anywhere
// inside v (templates and wildcards are not literal ids).
func dashboardRefs(v any, underRefKey bool, entities, types map[string]bool) {
	switch t := v.(type) {
	case string:
		if underRefKey && entityIDPattern.MatchString(t) {
			entities[t] = true
		}
	case []any:
		for _, item := range t {
			dashboardRefs(item, underRefKey, entities, types)
		}
	case map[string]any:
		for k, item := range t {
			if text, isText := item.(string); k == "type" && isText && strings.HasPrefix(text, "custom:") {
				types[text] = true
			}
			dashboardRefs(item, entityRefKeys[k], entities, types)
		}
	}
}

// entityPresence answers whether an entity a card names exists: "" when it
// has a state, else why not.
type entityPresence func(entityID string) string

// dashboardIssues lists every card that references an entity without a state
// or a custom type no installed resource seems to provide.
func dashboardIssues(cfg map[string]any, present entityPresence, resources []lovelaceResource) []dashboardIssue {
	var issues []dashboardIssue
	for _, p := range placedCards(cfg) {
		entities, types := map[string]bool{}, map[string]bool{}
		dashboardRefs(p.value, false, entities, types)
		for _, id := range slices.Sorted(maps.Keys(entities)) {
			if why := present(id); why != "" {
				issues = append(issues, dashboardIssue{Kind: "missing_entity", Where: p.where, EntityID: id, Detail: why})
			}
		}
		for _, cardType := range slices.Sorted(maps.Keys(types)) {
			if providedBy(cardType, resources) == "" {
				issues = append(issues, dashboardIssue{Kind: "card_type_unconfirmed", Where: p.where, CardType: cardType,
					Detail: "no installed dashboard resource seems to provide it (missing, or loaded another way)"})
			}
		}
	}
	return issues
}

// customTypes lists the custom: types a config uses, sorted.
func customTypes(cfg map[string]any) []string {
	entities, types := map[string]bool{}, map[string]bool{}
	dashboardRefs(cfg, false, entities, types)
	return slices.Sorted(maps.Keys(types))
}

// nonProviderDirs are URL folders that name no card.
var nonProviderDirs = map[string]bool{"": true, "local": true, "hacsfiles": true, "community": true, "www": true, "static": true}

// layoutCardTypes: layout-card registers view layouts under other names.
// ponytail: one known alias set; add another only for a popular card whose
// file name does not match its type.
var layoutCardTypes = map[string]bool{"masonry-layout": true, "grid-layout": true, "horizontal-layout": true, "vertical-layout": true}

// providedBy returns the resource URL that plausibly provides a custom:
// type, judged by the file and folder names of each resource URL (a
// heuristic: Home Assistant cannot list registered card types).
func providedBy(cardType string, resources []lovelaceResource) string {
	name := strings.ToLower(strings.TrimPrefix(cardType, "custom:"))
	for _, r := range resources {
		u, err := url.Parse(r.URL)
		if err != nil {
			continue
		}
		dir, file := path.Split(strings.TrimRight(u.Path, "/"))
		stem := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(file, ".js"), ".mjs"), ".min"))
		folder := strings.ToLower(path.Base(strings.TrimRight(dir, "/")))
		for _, candidate := range []string{stem, strings.TrimSuffix(stem, "-bundle"), folder, strings.TrimPrefix(folder, "lovelace-")} {
			if nonProviderDirs[candidate] {
				continue
			}
			if name == candidate || strings.HasPrefix(name, candidate+"-") || (candidate == "layout-card" && layoutCardTypes[name]) {
				return r.URL
			}
		}
	}
	return ""
}

// readResources reads the installed dashboard resources.
func readResources(s *relaySession) ([]lovelaceResource, error) {
	var resources []lovelaceResource
	err := s.wsDecode(map[string]any{"type": "lovelace/resources"}, &resources, "the dashboard resources")
	return resources, err
}

// presenceFrom builds the entity check from one states read and the full
// registry.
func presenceFrom(states []stateRow, rows []fullRegistryRow) entityPresence {
	hasState := map[string]bool{}
	for _, st := range states {
		hasState[st.EntityID] = true
	}
	registry := map[string]fullRegistryRow{}
	for _, row := range rows {
		registry[row.EntityID] = row
	}
	return func(id string) string {
		row, registered := registry[id]
		switch {
		case hasState[id]:
			return ""
		case registered && row.DisabledBy != "":
			return "disabled (by " + row.DisabledBy + ")"
		case registered:
			return "registered but has no state (not loaded)"
		}
		return "not in Home Assistant"
	}
}

// canonicalJSON is the content identity of a config: key order ignored,
// numbers kept exact.
func canonicalJSON(raw json.RawMessage) string {
	if raw == nil {
		return "absent"
	}
	v, err := decodeJSONNumber(raw)
	if err != nil {
		return "unreadable"
	}
	out, _ := json.Marshal(v) // map keys sort
	return string(out)
}
