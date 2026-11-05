package main

import (
	"strings"
	"testing"
)

// A to-do remove or update names the items by their summaries, read from
// the list when the plan is made, never by uid (#929).
func TestTodoItemConfirmationNamesTheItem(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lang     string
		failList bool
		listener bool // an automation listens to the list: checking off asks too
		args     []string
		want     string
	}{
		{name: "remove by uid", args: []string{"todo.remove_item", "--set", "item=79d4a1"},
			want: "I'm about to remove “Milk” from “Shopping list”."},
		{name: "remove by uid and by name", args: []string{"todo.remove_item", "--set", "item=[\"79d4a1\",\"Eggs\"]"},
			want: "I'm about to remove “Milk” and “Eggs” from “Shopping list”."},
		{name: "a uid the list doesn't hold", args: []string{"todo.remove_item", "--set", "item=0000"},
			want: "I'm about to remove an item from “Shopping list”."},
		{name: "two uids the list doesn't hold", args: []string{"todo.remove_item", "--set", "item=[\"0000\",\"1111\"]"},
			want: "I'm about to remove an item and 1 more from “Shopping list”."},
		{name: "a named item and one the list doesn't hold", args: []string{"todo.remove_item", "--set", "item=[\"79d4a1\",\"0000\"]"},
			want: "I'm about to remove “Milk” and 1 more from “Shopping list”."},
		{name: "a list that can't be read", failList: true, args: []string{"todo.remove_item", "--set", "item=79d4a1"},
			want: "I'm about to remove an item from “Shopping list”."},
		{name: "check off by uid", listener: true, args: []string{"todo.update_item", "--set", "item=79d4a1", "--set", "status=completed"},
			want: "change “Milk” on “Shopping list”: status set to completed."},
		{name: "German", lang: "de", args: []string{"todo.remove_item", "--set", "item=79d4a1"},
			want: "Ich werde gleich „Milk“ aus „Shopping list“ entfernen."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			shoppingList(f, "Milk")
			f.fail["todo/item/list"] = tc.failList
			if tc.listener {
				listUnlocks(f)
			}
			if tc.lang != "" {
				f.config = map[string]any{"language": tc.lang}
			}
			args := append([]string{tc.args[0], "todo.shopping_list", "--skill", "ha-nova:todo"}, tc.args[1:]...)
			exit, env, _, _ := runPlanForTest(t, f, args...)
			if exit != 3 || !strings.Contains(env.Say, tc.want) || strings.Contains(env.Say, "79d4") {
				t.Fatalf("exit %d say %q, want it to say %q\n%+v", exit, env.Say, tc.want, env)
			}
		})
	}
}

// shoppingList: “Shopping list” holding first (uid 79d4a1) and “Eggs”.
func shoppingList(f *fakeHA, first string) {
	f.ent("todo.shopping_list", "shopping_list", "2", map[string]any{"friendly_name": "Shopping list"})
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint != "ws" || p["type"] != "todo/item/list" || f.fail["todo/item/list"] {
			return nil, false
		}
		return map[string]any{"items": []any{
			map[string]any{"uid": "79d4a1", "summary": first, "status": "needs_action"},
			map[string]any{"uid": "b2e0c7", "summary": "Eggs", "status": "needs_action"},
		}}, true
	}
}

// listUnlocks: an automation that unlocks the front door when the list
// changes, so every item change asks with a code.
func listUnlocks(f *fakeHA) {
	f.events = []map[string]any{{"event": "shopping_list_updated", "listener_count": 1}}
	f.automation("groceries", "g1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "event", "event_type": "shopping_list_updated"}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	})
}

// The item's name is read once, when the plan is made, and sealed with it:
// apply asks in the window with the name the plan showed, even after the
// item was renamed.
func TestTodoItemNameStaysWithThePlan(t *testing.T) {
	f := newFakeHA()
	shoppingList(f, "Milk")
	listUnlocks(f)
	h := newWriteHarness(t, f)
	w := useWindow(t, windowConfirmed)
	env := h.plan("todo.update_item", "todo.shopping_list", "--skill", "ha-nova:todo", "--set", "item=79d4a1", "--set", "status=completed")
	if env.Tier != "typed" {
		t.Fatalf("tier %q: %q", env.Tier, env.Say)
	}
	shoppingList(f, "Oat milk")
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || len(w.messages) != 1 || !strings.Contains(w.messages[0], "change “Milk” on “Shopping list”") || strings.Contains(w.messages[0], "79d4") {
		t.Fatalf("apply: exit %d %+v window %q", exit, applied.Error, w.messages)
	}
}

// The phone title keeps the list's name and counts every item: long item
// names are cut and the rest counted, within one lock-screen line.
func TestTodoItemPhoneTitleKeepsTheListAndTheCount(t *testing.T) {
	long := sayText(strings.Repeat("Organic oat milk, barista edition ", 3), true)
	goal := actionGoal{verb: "todo_remove", names: []string{"“Shopping list”"}, items: []string{long, long, long}, more: 1}
	title := goal.title("en", "")
	if len([]rune(title)) > phoneTitleLimit || !strings.Contains(title, "“Shopping list”") || !strings.Contains(title, "and 3 more") {
		t.Fatalf("title %q (%d runes)", title, len([]rune(title)))
	}
	if short := (actionGoal{verb: "todo_remove", names: []string{"“Shopping list”"}, items: []string{"“Milk”"}}).title("en", ""); short != "Remove “Milk” from “Shopping list”?" {
		t.Fatalf("short title %q", short)
	}
}
