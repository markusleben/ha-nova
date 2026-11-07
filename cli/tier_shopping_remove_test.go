package main

import "testing"

// Calmer confirmations item 7: removing up to 5 items, each on the
// built-in shopping list as read now, runs at once; anything else keeps the
// preview.
func TestShoppingListRemovalOfListedItemsRunsAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		item     string
		failList bool
		listener bool
		tier     string
	}{
		{name: "one listed item by name", platform: "shopping_list", item: "Milk", tier: "ordinary"},
		{name: "two listed items by uid and name", platform: "shopping_list", item: `["79d4a1","Eggs"]`, tier: "ordinary"},
		{name: "an item the list doesn't hold", platform: "shopping_list", item: "Bread", tier: "preview"},
		{name: "one listed and one missing", platform: "shopping_list", item: `["Milk","Bread"]`, tier: "preview"},
		{name: "the same item twice", platform: "shopping_list", item: `["Milk","Milk"]`, tier: "preview"},
		{name: "more than five items", platform: "shopping_list", item: `["Milk","Eggs","Milk","Eggs","Milk","Eggs"]`, tier: "preview"},
		{name: "a list that can't be read", platform: "shopping_list", item: "Milk", failList: true, tier: "preview"},
		{name: "another to-do platform", platform: "local_todo", item: "Milk", tier: "preview"},
		{name: "a listener that unlocks keeps its code", platform: "shopping_list", item: "Milk", listener: true, tier: "typed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			todoList(f, tc.platform, "Milk")
			f.fail["todo/item/list"] = tc.failList
			if tc.listener {
				listUnlocks(f)
			}
			_, env, _, _ := runPlanForTest(t, f, "todo.remove_item", "todo.shopping_list", "--skill", "ha-nova:todo", "--set", "item="+tc.item)
			if env.Tier != tc.tier {
				t.Fatalf("tier %q, want %q: %+v", env.Tier, tc.tier, env)
			}
		})
	}
}
