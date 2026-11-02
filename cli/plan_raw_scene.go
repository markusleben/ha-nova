package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// A scene save replaces the whole scene (Home Assistant's config/scene.py,
// _write_value): a member the body leaves out is gone. It is checked against
// the live scene at planning and at apply's recompute, and the scene enters
// the plan's basis, so an edit since the preview is drift.

// rawDrop: a save that would drop what the live scene holds; hint says how
// to keep it (computeRawPlan's next_step).
type rawDrop struct{ message, hint string }

func (r rawDrop) Error() string { return r.message }
func (r rawDrop) next() string  { return r.hint }

var rawSlugGaps = regexp.MustCompile(`[^a-z0-9]+`)

// sceneKept: a save on an existing scene that drops a member needs a
// backups save of exactly its live config earlier in the plan
// (skills/scene → Update). Keeps the live config, or "absent" for a new
// scene, for the basis. A config that can't be read is an error: nothing is
// saved over unseen.
func (n *rawNamer) sceneKept(earlier []rawRequest, payload map[string]any) (refusal, err error) {
	target, _ := payload["path"].(string)
	target, _, _ = strings.Cut(target, "#")
	target, _, _ = strings.Cut(target, "?")
	if payload["method"] != http.MethodPost || !rawSceneConfigPath.MatchString(target) {
		return nil, nil
	}
	id := target[strings.LastIndex(target, "/")+1:]
	if _, again := n.scenes[id]; again { // each save is checked against the live scene, not the one before it
		return rawDrop{message: "a plan saves the scene " + id + " once", hint: "Put every change to the scene into one save of the whole scene."}, nil
	}
	data, err := rawRead(n.s, "core", map[string]any{"method": http.MethodGet, "path": target})
	var core struct {
		Status int            `json:"status"`
		Body   map[string]any `json:"body"`
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		err = dec.Decode(&core)
	}
	if err == nil && core.Status != http.StatusNotFound && (core.Status != http.StatusOK || core.Body == nil) {
		err = fmt.Errorf("Home Assistant answered HTTP %d", core.Status)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the scene %s's config, so nothing was planned or sent: %w", id, err)
	}
	if n.scenes == nil {
		n.scenes = map[string]any{}
	}
	if core.Status == http.StatusNotFound {
		n.scenes[id] = "absent"
		return nil, nil
	}
	live := numbersByValue(core.Body)
	n.scenes[id] = live
	body, _ := payload["body"].(map[string]any)
	entities, _ := body["entities"].(map[string]any)
	kept := map[string]bool{}
	for member := range entities {
		kept[strings.ToLower(member)] = true
	}
	was, _ := core.Body["entities"].(map[string]any)
	var dropped []string
	for member := range was {
		if !kept[strings.ToLower(member)] {
			dropped = append(dropped, member)
		}
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	for _, item := range earlier {
		saved, _ := decodeObject(item.Payload)
		if item.Endpoint == "backups" && saved["action"] == "save" && jsonEqual(numbersByValue(saved["data"]), live) {
			return nil, nil
		}
	}
	slices.Sort(dropped)
	members, slug := strings.Join(dropped, ", "), strings.Trim(rawSlugGaps.ReplaceAllString(strings.ToLower(id), "-"), "-")
	return rawDrop{
		message: fmt.Sprintf("saving the scene %s would remove %s from it, with no copy of its config saved first", id, members),
		hint: fmt.Sprintf("This would remove %s from the scene: a save replaces the whole scene. To keep them, read the scene (core GET /api/config/scene/config/%s) and send every member with the change. "+
			`If the user wants them removed, tell them which ones go, then plan again with a backups save of its current config first: {"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-%s","data":<the body of that read, unchanged>}}.`,
			members, id, strings.Trim(slug[:min(len(slug), 59)], "-")),
	}, nil
}
