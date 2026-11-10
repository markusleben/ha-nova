package main

import "strings"

// todoItemGoals: the to-do services whose item field names list items by
// uid or summary, and the goal that names those items (#929).
var todoItemGoals = map[string]string{"todo.remove_item": "todo_remove", "todo.update_item": "todo_update"}

// todoItemWordPrefix keys an item's summary in plan.Words by the reference
// the call sends ("todo_item:<uid or summary>").
const todoItemWordPrefix = "todo_item:"

// todoItemWords: the summary of each item a to-do call names, read from
// the list when the plan is made (todo/item/list), for plan.Words, where
// they are sealed with the plan, so apply asks in the same words. A re-plan
// keeps the stored ones. A reference the list doesn't hold, or a list that
// can't be read, gets none: the confirmation says "an item".
func todoItemWords(s *relaySession, plan storedPlan) haWords {
	refs := todoItemRefs(plan)
	found := haWords{}
	if len(refs) == 0 {
		return found
	}
	if s.lang != "" { // a re-plan: the words the human read
		for _, ref := range refs {
			if summary := s.words[todoItemWordPrefix+ref]; summary != "" {
				found[todoItemWordPrefix+ref] = summary
			}
		}
		return found
	}
	var reply struct {
		Items []struct {
			UID     string `json:"uid"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	if s.wsDecode(map[string]any{"type": "todo/item/list", "entity_id": plan.Action.EntityIDs[0]}, &reply, "to-do items") != nil {
		return found
	}
	for _, ref := range refs {
		for _, item := range reply.Items { // as Home Assistant finds it: the first item by uid or summary
			if (item.UID == ref || item.Summary == ref) && item.Summary != "" {
				found[todoItemWordPrefix+ref] = sayTextUpTo(item.Summary, 120, false)
				break
			}
		}
	}
	return found
}

// todoItemRefs: the item references of a to-do call on one list, in order.
func todoItemRefs(plan storedPlan) []string {
	if todoItemGoals[plan.Action.Service] == "" || len(plan.Action.EntityIDs) != 1 {
		return nil
	}
	var refs []string
	switch item := plan.Action.Data["item"].(type) {
	case string:
		refs = append(refs, item)
	case []any:
		for _, ref := range item {
			if text, ok := ref.(string); ok {
				refs = append(refs, text)
			}
		}
	}
	return refs
}

// todoItemGoal: "remove “Milk” from “Shopping list”", the items by their
// summaries, the ones without one counted (itemList), so the sentence
// never understates how many go.
func todoItemGoal(lang string, plan storedPlan, names []string) (actionGoal, bool) {
	verb := todoItemGoals[plan.Action.Service]
	refs := todoItemRefs(plan)
	if verb == "" || len(refs) == 0 || len(names) != 1 {
		return actionGoal{}, false
	}
	goal := actionGoal{verb: verb, names: names}
	for _, ref := range refs {
		if item := sayText(plan.Words[todoItemWordPrefix+ref], true); item != "" {
			goal.items = append(goal.items, item)
		} else {
			goal.more++
		}
	}
	return goal, true
}

// itemList: the named to-do items, then how many more ("“Milk” and 2
// more"); none named, "an item" ("an item and 2 more"). A typed plan's
// (all) names every named one.
func (g actionGoal) itemList(lang string) string {
	items, more := g.items, g.more
	if len(items) > 3 && !g.all {
		items, more = items[:3], more+len(items)-3
	}
	switch {
	case len(items) == 0 && more <= 1:
		return confirmT(lang, "noun.todo_item_unknown")
	case len(items) == 0:
		return confirmT(lang, "list.more", "list", confirmT(lang, "noun.todo_item_unknown"), "count", more-1)
	case more == 0:
		return sayAll(lang, items)
	}
	return confirmT(lang, "list.more", "list", strings.Join(items, ", "), "count", more)
}

// todoItemField: the call field todoItemGoal already says in words.
func todoItemField(plan storedPlan, field string) bool {
	return field == "item" && len(todoItemRefs(plan)) > 0
}
