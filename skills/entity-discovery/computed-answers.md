# Entity Discovery — Computed Answers

On-demand part of `../entity-discovery/SKILL.md`. Read it before answering a
question that needs math over current states: an average, a total, the
highest or lowest ("average temperature upstairs", "how much power is used
right now", "which room is warmest"). How MANY things are on or open stays a
`ha-nova find` count (its exact `total`) or a State Snapshot query; never
count by hand. Values over time belong to `ha-nova:history`.

1. **Place and kind first.** The place: an area id from WS
   `config/area_registry/list`, a floor id from WS
   `config/floor_registry/list` (the template functions also take the exact
   name). The kind: domain plus `device_class` (`sensor` + `temperature`,
   `humidity`, `power`, `energy`, ...). Unclear place or kind: ask. Never
   guess an id, and never stand in an entity of another kind. When unsure
   what exists, list that kind there first (`ha-nova find --domain sensor
   --area <area_id> --limit 200`; narrow until `returned` equals `total`).
   Leave out diagnostic sensors (a chip or battery temperature): in WS
   `config/entity_registry/list_for_display`, a row's `ec` is an index into
   the response's `entity_categories`; skip only rows where it maps to
   `diagnostic`, put those ids in `skip`, and name them in the answer.
2. **Render once.** `ha-nova relay core --method POST --path /api/template
   --body-file <payload-file>` with `{"template": "..."}`; the result string
   is in `.data.body`. A template only reads; it cannot call a service. One
   render selects the set and returns its inputs. Swap in
   `area_entities('<area_id>')` for an area, `states.sensor | map(attribute='entity_id') | list`
   for the whole home, the class you need, and for the result
   `map(attribute=1) | sum`, or `(ns.ok | max(attribute=1)) if ns.ok else none`
   (`min` the same; the whole row, so name and room come with it):

   ```jinja
   {%- set ids = floor_entities('<floor_id>') -%}
   {%- set skip = [] -%}
   {%- set keep = [] -%}
   {%- set ns = namespace(ok=[], bad=[], groups=[]) -%}
   {%- for s in states.sensor | selectattr('entity_id', 'in', ids) | rejectattr('entity_id', 'in', skip) | selectattr('attributes.device_class', 'eq', 'temperature') -%}
   {%- if s.attributes.entity_id is defined and s.entity_id not in keep -%}{%- set ns.groups = ns.groups + [s.name] -%}
   {%- elif s.state | is_number -%}{%- set ns.ok = ns.ok + [[s.name, s.state | float, s.attributes.get('unit_of_measurement'), area_name(s.entity_id)]] -%}
   {%- else -%}{%- set ns.bad = ns.bad + [s.name] -%}{%- endif -%}
   {%- endfor -%}
   {{ {'inputs': ns.ok, 'unreadable': ns.bad, 'groups_left_out': ns.groups, 'result': ns.ok | map(attribute=1) | average(none)} | to_json }}
   ```

   A group sensor is left out (its members are counted on their own, and
   some may sit elsewhere); name it if it would have answered the question.
   A helper that already computes the answer (a min/max or group helper the
   user names, or the only one for that place) goes in `keep` and is
   the only id in `ids`: the answer comes from it alone.
3. **Check before answering.** No inputs: say nothing of that kind reports
   there, and name the kind you looked for. More than one unit in `inputs`
   (°C and °F, W and kW): do not combine them; say so. "Which room is
   warmest": when areas have their own temperature sensor
   (`temperature_entity_id` in the area list), build the rows from that list
   and carry each AREA's name (the sensor may sit in another area), not
   `area_name` of the sensor. Power for the whole home: a sensor that
   measures the home's total use, when one exists. The grid sensor alone is
   that only without solar or a battery; with them, home use is grid plus
   solar plus battery flow, from the live power sensors in the Energy
   settings (WS `energy/get_prefs`; `ha-nova:energy` knows each field and
   its sign). If that cannot be built, say the exact total is not known
   rather than give the grid number. A plain sum over several meters
   assumes none of them measures another (a whole-home meter plus plugs
   counts twice): say that.
4. **Answer.** The result first, then the inputs in plain words, names as
   written: "21.3 °C on average upstairs, from 3 sensors: Bedroom 21.0,
   Office 22.1, Bath 20.8. Kids Room could not be read." Every name in
   `unreadable` and `skip` is named; never drop one to make the answer look
   complete. If the user wants this number to stay, offer once to make it a
   helper (`ha-nova:helper`).
