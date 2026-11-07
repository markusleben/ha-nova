package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
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

// copyNext: what to do when a copy a save or update needs can't be saved
// or would be left out (executeRaw); removes says what it keeps (copyNeeded).
func copyNext(removes string) string {
	return "Tell the user there is no copy, and this change removes " + removes + ", so it doesn't go ahead without one. " +
		"Once the copy can be saved (a full snapshot store can be pruned), apply this plan again, never with --no-snapshot."
}

// rawSceneSave: the scene id a request saves; "" for any other request.
func rawSceneSave(payload map[string]any) string {
	raw, _ := payload["path"].(string)
	target, _ := relayCorePath(raw)
	method, _ := payload["method"].(string) // the relay's guard sends any case
	if strings.ToUpper(strings.TrimSpace(method)) != http.MethodPost || !rawSceneConfigPath.MatchString(target) {
		return ""
	}
	return target[strings.LastIndex(target, "/")+1:]
}

// sceneKept: a scene save keeps the body's id as the path's (Home Assistant
// stores the body's), and one on an existing scene that drops a member needs
// a backups save of exactly its live config earlier in the plan
// (skills/scene → Update). Keeps the live config, or "absent" for a new
// scene, for the basis, and what the save drops for its line (sceneNote).
// A config that can't be read is an error: nothing is saved over unseen.
func (n *rawNamer) sceneKept(earlier []rawRequest, payload map[string]any) (refusal, err error) {
	id := rawSceneSave(payload)
	body, _ := payload["body"].(map[string]any)
	bodyID, setsID := body["id"]
	switch _, again := n.scenes[id]; {
	case id == "":
		return nil, nil
	case setsID && bodyID != id:
		return rawDrop{message: fmt.Sprintf("the body's id %s is not the scene id %s in the path", jsonText(bodyID), id),
			hint: "Send the body's id exactly as the id in the path, or leave it out: Home Assistant stores the body's id, so another one renames the scene."}, nil
	case again: // each save is checked against the live scene, not the one before it
		return rawDrop{message: "a plan saves the scene " + id + " once", hint: "Put every change to the scene into one save of the whole scene."}, nil
	}
	live, err := readScene(n.s, id)
	if err != nil {
		return nil, fmt.Errorf("cannot read the scene %s's config, so nothing was planned or sent: %w", id, err)
	}
	if n.scenes == nil {
		n.scenes, n.dropped = map[string]any{}, map[string][]string{}
	}
	n.scenes[id] = live
	entities, _ := body["entities"].(map[string]any)
	kept := map[string]bool{}
	for member := range entities {
		kept[strings.ToLower(member)] = true
	}
	config, _ := live.(map[string]any)
	was, _ := config["entities"].(map[string]any)
	var dropped []string
	for member := range was {
		if !kept[strings.ToLower(member)] {
			dropped = append(dropped, member)
		}
	}
	slices.Sort(dropped)
	if len(dropped) == 0 {
		return nil, nil
	}
	members := strings.Join(dropped, ", ")
	removal := fmt.Sprintf("saving the scene %s would remove %s from it", id, members)
	if hidden := hiddenRemoval(live, removal); hidden != nil {
		return hidden, nil
	}
	if copySaved(earlier, live) {
		n.dropped[id] = dropped
		return nil, nil
	}
	return rawDrop{
		message: removal + ", with no copy of its config saved first",
		hint: fmt.Sprintf("This would remove %s from the scene: a save replaces the whole scene. If the user wants them removed (swapping a member for another removes the old one), tell them which ones go, "+
			`then plan it with a backups save of its current config first (ha-nova plan raw): {"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"%s","data":<the body of core GET /api/config/scene/config/%s, unchanged>}}. `+
			"If they were left out by mistake, read that config and send every member with the change.",
			members, autoName(id), id),
	}, nil
}

// copySaved: a backups save of exactly live comes earlier in the plan, and
// no delete or prune after it may take the copy.
func copySaved(earlier []rawRequest, live any) bool {
	for i, item := range earlier {
		saved, _ := decodeObject(item.Payload)
		pruned := slices.ContainsFunc(earlier[i+1:], func(later rawRequest) bool {
			action, _ := decodeObject(later.Payload)
			return later.Endpoint == "backups" && (action["action"] == "delete" || action["action"] == "prune")
		})
		if item.Endpoint == "backups" && saved["action"] == "save" && !pruned && jsonEqual(numbersByValue(saved["data"]), live) {
			return true
		}
	}
	return false
}

// hiddenRemoval: a removal from live that holds a value HA NOVA hid is
// refused outright: a copy never keeps a hidden value (guardRaw), so none
// could bring it back.
func hiddenRemoval(live any, removal string) error {
	if text, _ := json.Marshal(live); !holdsHiddenValue(text) {
		return nil
	}
	return rawDrop{message: removal + ", and it holds a value HA NOVA hid, which no copy can keep",
		hint: "Tell the user HA NOVA can't make this removal: it holds a code or key HA NOVA hides, so no copy could bring it back. They can make the change in Home Assistant."}
}

// autoName: the name of the config copy of id (skills/ha-nova/config-snapshots.md
// → Capture), within the store's 64 characters.
func autoName(id string) string {
	slug := strings.Trim(rawSlugGaps.ReplaceAllString(strings.ToLower(id), "-"), "-")
	return strings.ReplaceAll("auto-"+strings.Trim(slug[:min(len(slug), 59)], "-"), `"`, "") // a slug holds no quote; said so for the hints that quote it
}

// relayKept is kept for ha-nova relay core|ws, after the shared guard
// (ha_read sends no write): one request, so no copy comes first, and a
// scene save or guarded update (rawReplaced) that drops what the live one
// holds is refused with the plan that keeps a copy. A ws body is checked as
// the Relay may send it: itself, and a collect_events envelope's message.
func relayKept(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (refusal, err error) {
	payload, _ := decodeObject(body)
	message, _ := payload["message"].(map[string]any)
	var requests []map[string]any
	for _, request := range []map[string]any{payload, message} {
		if _, _, _, guarded := rawReplaceTarget(endpoint, request); guarded || endpoint == "core" && rawSceneSave(request) != "" {
			requests = append(requests, request)
		}
	}
	if len(requests) == 0 {
		return nil, nil
	}
	s, err := newRelaySession(paths, opts)
	if err != nil {
		return nil, err
	}
	defer s.close()
	for _, request := range requests {
		what, _ := request["type"].(string)
		if collection, _ := rawHelperUpdate(endpoint, request); collection != "" {
			what = "helper update"
		}
		if endpoint == "core" {
			what = "scene save"
		}
		refusal, err = newRawNamer(s, nil).kept(nil, endpoint, request)
		var drop rawDrop
		if errors.As(refusal, &drop) { // the relay prints the error alone
			return fmt.Errorf("%v. %s", rawRefusal(what, drop.message), drop.hint), nil
		}
		if refusal != nil || err != nil {
			return refusal, err
		}
	}
	return nil, nil
}

// readScene: the scene id's live config with numbers by value (the plan's
// basis), or "absent".
func readScene(s *relaySession, id string) (any, error) {
	data, err := rawRead(s, "core", map[string]any{"method": http.MethodGet, "path": "/api/config/scene/config/" + id})
	var core struct {
		Status int            `json:"status"`
		Body   map[string]any `json:"body"`
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		err = dec.Decode(&core)
	}
	switch {
	case err != nil:
		return nil, err
	case core.Status == http.StatusNotFound:
		return "absent", nil
	case core.Status != http.StatusOK || core.Body == nil:
		return nil, fmt.Errorf("Home Assistant answered HTTP %d", core.Status)
	}
	return numbersByValue(core.Body), nil
}

// sceneUnchanged runs right before a scene save is sent: the scene must
// still be the one the plan's basis holds (apply's recompute read it before
// any step, and a step since may have let another client change it).
func sceneUnchanged(s *relaySession, payload, basis map[string]any) (refusal, err error) {
	id := rawSceneSave(payload)
	if id == "" {
		return nil, nil
	}
	live, err := readScene(s, id)
	if err != nil {
		return nil, fmt.Errorf("cannot read the scene %s's config again before its save: %w", id, err)
	}
	if scenes, _ := basis["scenes"].(map[string]any); !jsonEqual(live, scenes[id]) {
		return fmt.Errorf("the scene %s changed in Home Assistant since the preview, so it was not saved over", id), nil
	}
	return nil, nil
}

// sceneNote: the sentence after a scene save's line naming the members it
// removes (a copy comes first: sceneKept); "" for any other request.
func (n *rawNamer) sceneNote(lang string, payload map[string]any) string {
	dropped := n.dropped[rawSceneSave(payload)]
	if len(dropped) == 0 {
		return ""
	}
	var names []string
	for _, id := range dropped {
		names = append(names, n.entity(id))
	}
	return confirmT(lang, "raw.scene_drops", "members", sayList(lang, names))
}

// copyNeeded: what a scene save or guarded update in items removes that
// only one of the leading copies keeps (kept without them), so apply never
// goes on without it; one that can't be read counts. "" when none does.
func copyNeeded(s *relaySession, items []rawRequest) (removes string) {
	kept, n := items[len(rawCopies(items)):], newRawNamer(s, nil)
	for i, item := range kept {
		payload, _ := decodeObject(item.Payload)
		if refusal, err := n.kept(kept[:i], item.Endpoint, payload); refusal != nil || err != nil {
			guard, _, _, _ := rawReplaceTarget(item.Endpoint, payload)
			if rawFileChange(item.Endpoint, payload) != "" {
				guard.removes = "the current content of a file"
			}
			return cmp.Or(guard.removes, "members from a scene")
		}
	}
	return ""
}
