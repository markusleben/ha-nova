# HA NOVA Energy Reference

Single source of truth for Energy dashboard schemas, KPI formulas, and analysis recipes.
Verified against Home Assistant core source (2026-07) and a live instance.

## Preferences Schema (`energy/get_prefs` → `energy/save_prefs`)

Top-level keys: `energy_sources[]`, `device_consumption[]`, `device_consumption_water[]` (HA 2025.12+).
`save_prefs` semantics: every key present in the message replaces its ENTIRE list; omitted keys stay untouched. There is no per-item merge. Persistence is debounced (~60 s) but the in-memory config updates immediately.

Canonical save message — only the touched key, as its complete merged list:

```json
{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.washer_energy","name":"Washing machine"}]}
```

Never echo the whole `get_prefs` object back as the save body — that turns a one-key edit into a full replace of all three lists. The save response echoes the new prefs in `.data`; verification is still the re-read.

### Grid — two schema generations

Legacy nested (HA ≤ 2026.2):

```json
{"type":"grid","cost_adjustment_day":0,
 "flow_from":[{"stat_energy_from":"sensor.import","stat_cost":null,"entity_energy_price":null,"number_energy_price":null}],
 "flow_to":[{"stat_energy_to":"sensor.export","stat_compensation":null,"entity_energy_price":null,"number_energy_price":null}]}
```

Flat (HA 2026.3+, storage-migrated automatically):

```json
{"type":"grid","cost_adjustment_day":0,
 "stat_energy_from":"sensor.import","stat_cost":null,
 "entity_energy_price":"sensor.price","number_energy_price":null,
 "stat_energy_to":"sensor.export","stat_compensation":null,
 "entity_energy_price_export":null,"number_energy_price_export":null}
```

Detection: `flow_from`/`flow_to` present = legacy; flat keys = new. Emit the generation you read; a mixed or wrong-generation payload is rejected by the schema. Flat grid allows import-only or export-only (the other stat `null`), but at least one stat is required. Grid statistic IDs must be unique across all grid sources.

### Other source types

```json
{"type":"solar","stat_energy_from":"sensor.pv","config_entry_solar_forecast":["<config_entry_id>"]}
{"type":"battery","stat_energy_from":"sensor.batt_out","stat_energy_to":"sensor.batt_in"}
{"type":"gas","stat_energy_from":"sensor.gas","stat_cost":null,"entity_energy_price":null,"number_energy_price":null}
```

`water` mirrors `gas`. Optional newer fields — preserve verbatim, add only on explicit request: `stat_rate`/`power_config` (live power, 2025.12–2026.2+), `stat_soc` (battery %, 2026.6+), `name` (2026.6+).

### Device consumption

```json
{"stat_consumption":"sensor.washer_energy","name":"Washing machine"}
```

Optional: `stat_rate` (live power), `included_in_stat` (2025.4+) = the parent's `stat_consumption` when this device's usage is already measured inside another tracked device (sub-metering). Core does NOT validate `included_in_stat` — the skill must check the parent exists in the same list and no cycle forms.

### Price and cost rules

- Per direction at most ONE of `entity_energy_price` (a price sensor) or `number_energy_price` (a fixed number).
- With a price field set and `stat_cost`/`stat_compensation` left `null`, HA auto-creates a cost sensor (`<energy_entity>_cost`/`_compensation`). Resolve the actual IDs via `{"type":"energy/info"}` → `cost_sensors`.
- Price changes apply forward-only; removed cost sensors leave their statistics behind.
- External statistics (ID contains `:`, e.g. `provider:energy_cost`) must not get entity/number price fields — HA 2026.4+ rejects the save unless `stat_cost` is already set.
- Price sensor unit must end in `/<energy unit>` (for example `EUR/kWh`).

## Validate Issue Types (`energy/validate`)

Response: `{energy_sources: Issue[][], device_consumption: Issue[][], device_consumption_water: Issue[][]}` — outer lists parallel to config order. `Issue = {type, affected_entities: [[entity_id, detail]], translation_placeholders}`.

| Issue type | Meaning → fix |
|---|---|
| `statistics_not_defined` | statistic ID has no data yet → check entity produces states; new sensors need up to an hour |
| `recorder_untracked` | excluded by recorder filter → adjust `recorder:` YAML config |
| `entity_not_defined` | entity does not exist → renamed or removed; update the config entry |
| `entity_unavailable` | entity currently unavailable → integration/device offline; config itself is fine |
| `entity_state_non_numeric` / `entity_negative_state` | bad sensor output → fix the source sensor |
| `entity_unexpected_device_class` | wrong `device_class` → energy sources need `energy` (gas: `energy`/`gas`; water: `water`; power: `power`) |
| `entity_unexpected_unit_*` | wrong unit family (energy: Wh/kWh/MWh…; power: W/kW; price: `/<unit>` suffix; gas/water volume units) |
| `entity_unexpected_state_class` | energy needs `total`/`total_increasing`; power/flow-rate needs `measurement` |
| `entity_state_class_measurement_no_last_reset` | `measurement` without `last_reset` cannot be an energy source |

## Statistics Contract (energy analysis)

All payloads in this document are WS request bodies — write them to a payload file and send via `ha-nova relay ws --data-file <payload-file> --out <result-file>`.

```json
{"type":"recorder/statistics_during_period","start_time":"<ISO>","end_time":"<ISO>",
 "statistic_ids":["sensor.x"],"period":"day","types":["change"],"units":{"energy":"kWh"}}
```

- Response: `{stat_id: [{start,end,change}]}` with `start`/`end` in **ms since epoch**; series are sparse — missing buckets, and statistic IDs entirely absent from the response, mean 0.
- `end_time` is boundary-inclusive: a bucket starting exactly at `end_time` is returned — drop buckets with `start >= end_time` or the current partial day inflates every total.
- `change` = consumption within the bucket, reset-safe. Never derive consumption by subtracting raw `total_increasing` states.
- Buckets align to HA-local midnight; weeks start Monday; DST days have 23/25 hours — window by local boundaries, never `date × 24 h`. Resolve HA's timezone via `GET /api/config` → `time_zone`; never assume the client machine's zone.
- Negative `change` is legal (meter adjustments, `total` class going down).
- `period: 5minute` reads short-term statistics — retained only ~10 days. Hourly and coarser is forever.
- Price/power sensors: request `types:["mean","min","max"]`; read the unit from `recorder/get_statistics_metadata` — never assume cents vs. main currency unit.
- Companion commands: `{"type":"energy/solar_forecast"}` → `{config_entry_id:{wh_hours:{iso:Wh}}}`; `energy/fossil_energy_consumption` (grid import × CO2 share).

## KPI Formulas (match the HA frontend)

Compute `used_total` per bucket, then sum every energy term over the whole window and apply each ratio ONCE to the window totals (matches the HA UI) — never average per-bucket percentages. Any denominator ≤ 0: report the KPI as not computable for that window, never Infinity/NaN. Per-bucket ratios are only for per-day charts.

- `used_total = grid_in + solar + battery_out − grid_out − battery_in`
- self-sufficiency % = `(1 − min(1, grid_in / max(0, used_total))) × 100`
- self-consumed solar % ≈ `(solar − grid_out) / solar × 100` — approximation; the HA UI additionally tracks whether battery charge came from solar or grid, so small deviations are expected. Label it.
- battery round-trip % = `(Σ battery_out / Σ battery_in) × 100` over ≥30 days (short windows are skewed by the state-of-charge delta)
- untracked = `used_total − Σ top-level device change` (exclude devices whose `included_in_stat` points at another tracked device). Frame as "unmetered devices + conversion losses" — battery homes always show a nonzero residual. Negative = devices over-reporting vs. the meter.

## Analysis Recipes

- **Coverage check (all recipes)**: if a statistic's first available bucket lies inside the analysis window, shrink the window or label the result partial-coverage — absent early buckets read as 0 and skew every KPI and comparison.
- **Device ranking**: per-device `change` over the window; share = device / `used_total`; rank; show "X of Y devices" when truncating.
- **Device cost (dynamic price)**: `Σ over hours (change(device, h) × mean(price, h))`. State the two approximations: flat consumption within each hour, and sub-hourly tariff slots averaged into the hourly mean. Verify the price statistic exists first; convert price units from metadata.
- **Device cost (fixed price)**: period `change` × `number_energy_price` from prefs.
- **Period comparison**: two windows, aligned to local boundaries, like-for-like (month-to-date vs. the same day count of the previous period; never a partial vs. a full month). Same period last year works — hourly statistics are kept forever.
- **Standby hunt**: per device `change` (kWh) between 02:00–05:00 local / 3 h → average standby kW; × 1000 = watts; rank; project cost per year via average price. Exclude duty-cycle appliances (fridge, freezer) from standby conclusions; skip DST-transition nights (the window is not 3 h).
- **Solar**: daily `change` trends, best/worst days. Forecast comparison: label deviation as "vs. revised forecast" — `forecast_solar`-style integrations update their forecast during the day, so this is not day-ahead accuracy.
- **EV via manager (evcc or similar)**: the manager's integration exposes its own statistics entities (charged kWh, solar share, average price per 30 d/365 d/total) — read those states directly. Cross-domain value the manager cannot provide: EV share of household consumption/cost = wallbox device `change` / `used_total`.

## Do Not

- No sub-hourly analysis further back than ~10 days (short-term statistics are purged).
- No peak-watts claims from energy statistics — only hourly averages are derivable; a real peak needs a recorded power sensor (`max`).
- No exact bill reconstruction — standing charges, VAT, and official metering differ; provider-supplied external cost statistics are authoritative over recomputation.
- No device-level disaggregation guesses from mains totals.
- No forecast accuracy scoring without a stored day-ahead snapshot.
