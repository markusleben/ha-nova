package main

// todoItemGoals: the to-do services whose item field names list items by
// uid or summary, and the goal that names those items (#929).
var todoItemGoals = map[string]string{"todo.remove_item": "todo_remove", "todo.update_item": "todo_update"}

// todoItemWordPrefix keys an item's summary in plan.Words by the reference
// the call sends ("todo_item:<uid or summary>").
const todoItemWordPrefix = "todo_item:"

// todoItemWords adds the summary of each item a to-do call names to words:
// read from the list when the plan is made (todo/item/list), kept in
// plan.Words, and sealed with them, so apply asks in the same words. A
// re-plan keeps the stored ones. A reference the list doesn't hold, or a
// list that can't be read, gets none: the confirmation says "an item".
func todoItemWords(s *relaySession, plan storedPlan, words haWords) haWords {
	refs := todoItemRefs(plan)
	if len(refs) == 0 {
		return words
	}
	found := haWords{}
	if s.lang != "" { // a re-plan: the words the human read
		for _, ref := range refs {
			if summary := s.words[todoItemWordPrefix+ref]; summary != "" {
				found[todoItemWordPrefix+ref] = summary
			}
		}
	} else {
		var reply struct {
			Items []struct {
				UID     string `json:"uid"`
				Summary string `json:"summary"`
			} `json:"items"`
		}
		if s.wsDecode(map[string]any{"type": "todo/item/list", "entity_id": plan.Action.EntityIDs[0]}, &reply, "to-do items") == nil {
			for _, ref := range refs {
				for _, item := range reply.Items {
					if (item.UID == ref || item.Summary == ref) && item.Summary != "" {
						found[todoItemWordPrefix+ref] = item.Summary
						break
					}
				}
			}
		}
	}
	if len(found) == 0 {
		return words
	}
	merged := haWords{}
	for key, word := range words {
		merged[key] = word
	}
	for key, word := range found {
		merged[key] = word
	}
	return merged
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
// summaries, each one without a summary as "an item".
func todoItemGoal(lang string, plan storedPlan, names []string) (actionGoal, bool) {
	verb := todoItemGoals[plan.Action.Service]
	refs := todoItemRefs(plan)
	if verb == "" || len(refs) == 0 || len(names) != 1 {
		return actionGoal{}, false
	}
	var items []string
	for _, ref := range refs {
		item := sayText(plan.Words[todoItemWordPrefix+ref], true)
		if item == "" {
			item = confirmT(lang, "noun.todo_item_unknown")
		}
		items = appendUnique(items, item)
	}
	return actionGoal{verb: verb, names: names, args: map[string]string{"items": sayList(lang, items)}}, true
}

// todoItemField: the call field todoItemGoal already says in words.
func todoItemField(plan storedPlan, field string) bool {
	return field == "item" && len(todoItemRefs(plan)) > 0
}
