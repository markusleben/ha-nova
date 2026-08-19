[.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}]
| {total: length, shown: (.[0:20] | length), omitted: ([length - 20, 0] | max), truncated: (length > 20), matches: .[0:20]}
