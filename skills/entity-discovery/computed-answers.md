# Entity Discovery — Computed Answers

On-demand part of `../entity-discovery/SKILL.md`. Read it before answering a
question that needs math over current states: an average, a total, the
highest or lowest ("average temperature upstairs", "how much power is used
right now", "which room is warmest"). How MANY things are on or open stays a
`ha-nova find` count (its exact `total`) or a State Snapshot query; never
count by hand. Values over time belong to `ha-nova:history`.

1. **Place and kind first.** Resolve the place: an area through `ha-nova
   find`'s `--area` ids or WS `config/area_registry/list`, a floor through WS
   `config/floor_registry/list`. Pick the kind: domain plus `device_class`
   (`sensor` + `temperature`, `humidity`, `power`, `energy`, ...). Unclear
   place or kind: ask. Never guess an id, and never stand in an entity of
   another kind. When unsure what exists, list that kind there first
   (`ha-nova find --domain sensor --area <area_id>`).
2. **Render once.** `ha-nova relay core --method POST --path /api/template
   --body-file <payload-file>` with `{"template": "..."}`; the result string
   is in `.data.body`. A template only reads; it cannot call a service. One
   render selects the set and returns its inputs. Swap in
   `area_entities('<area_id>')` for an area, the class you need, and for the
   result `map(attribute=1) | sum`, or `max(attribute=1)` /
   `min(attribute=1)` (the whole row, so the name comes with it):

   ```jinja
   {%- set ns = namespace(ok=[], bad=[]) -%}
   {%- for s in expand(floor_entities('<floor_id>')) | selectattr('domain', 'eq', 'sensor') | selectattr('attributes.device_class', 'eq', 'temperature') -%}
   {%- if s.state | is_number -%}{%- set ns.ok = ns.ok + [[s.name, s.state | float, s.attributes.unit_of_measurement]] -%}
   {%- else -%}{%- set ns.bad = ns.bad + [s.name] -%}{%- endif -%}
   {%- endfor -%}
   {{ {'inputs': ns.ok, 'unreadable': ns.bad, 'result': ns.ok | map(attribute=1) | average(none)} | to_json }}
   ```

   `expand` swaps a group for its members, so a group never counts twice.
3. **Check before answering.** No inputs: say nothing of that kind reports
   there, and name the kind you looked for. More than one unit in `inputs`
   (°C and °F, W and kW): do not combine them; say so. A helper that
   already computes this (for example a min/max helper) is one of the
   inputs: say so, or answer from it alone. A total over several meters
   assumes none of them measures another (a whole-home meter plus plugs
   counts twice): say that when one looks like a main meter.
4. **Answer.** The result first, then the inputs in plain words, names as
   written: "21.3 °C on average upstairs, from 3 sensors: Bedroom 21.0,
   Office 22.1, Bath 20.8. Kids Room could not be read." Every name in
   `unreadable` is named; never drop it to make the answer look complete.
   If the user wants this number to stay, offer once to make it a helper
   (`ha-nova:helper`).
