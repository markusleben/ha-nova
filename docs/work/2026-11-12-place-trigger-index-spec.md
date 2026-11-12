# Place-trigger index: room automations without a config read each

Status: active

Date: 2026-11-12. Issue MAR-149 (follows MAR-142, #1094). Owner: tier engine (`cli/tier_places.go`, `cli/tier_scan.go`, `cli/tier_ahead.go`, `cli/tier_raw_takeover.go`), new `cli/tier_place_index.go`. Target: 0.31 (release/0.31). High-risk: typed-tier path; a wrong skip under-asks.

## Today (stopgap A)

Home Assistant's `search/related` for an area or floor returns every automation of the devices and entities in it, not only those whose trigger targets that place. The consumer scan reads the loaded config of each automation found only through a place (`warmConfig`, WS `automation/config`) and drops those whose triggers target none of the entity's places (`targetsPlace`). These reads are outside the 100-config cap; the read deadline bounds them (a cut is typed). Bed, HA 2026.11.0, `plan call light.turn_on light.kitchen_lights` with 300 such automations: 0.10 s without them, 0.44 s with them.

## Change: a stamped index of trigger targets

A file, `<CacheDir>/place-index.json` (version, home scope, entries; at most 2,000 entries and 4 MB; written through a unique temp file and a rename), maps an automation entity id to:

- `stamp`: the `last_changed` of its state, only while that state is `on` or `off`;
- `triggers`: `{"target": …}` of each enabled trigger that has one (`flattenTriggers`), JSON as read, `null` targets kept. A missing or `null` list is no entry.

One engine method, `placeOnlyMiss(automation, places)`, replaces "warm the config, then `targetsPlace`" for automations found only through a place in `scanConsumers`, `readAhead`, and `rawCarry` (`tier_raw_takeover.go`). `quiet` (`tier_helper_options.go`) stays as it is. The method returns true only when the automation certainly has no trigger that targets one of `places`:

1. Stamp: the automation's state, if it is `on` or `off` with a `last_changed`; else no stamp.
2. No stamp, or this session has already sent the automation's config read: decide from that config, as today, and record nothing. An earlier config read may predate the state, so it proves no pair, and the session's own evidence wins over the disk.
3. A well-formed entry (each element exactly `{"target": …}`) for this home and build, whose stamp equals the current one and whose targets miss `places`, while the check's time isn't up: skip, with no config read. Past the deadline it falls to step 4, whose failed read is typed as before. A hit that targets a place is read live, for its scan.
4. Otherwise read the config now. The state answered before this read went out, so a reload in between only gives the config a stamp that is already stale. Record the pair, except for a config that still names `use_blueprint` (Home Assistant answers an instance already substituted; one that isn't depends on the blueprint file). An unreadable config returns false: it goes on as a consumer, typed as today.

The session loads the file once and saves it when it closes (`relaySession.close`). A missing, malformed, oversized, other-version, or other-home file is an empty index, and a failed write is ignored: both cost only reads. Two processes writing at once lose cache hits, never correctness.

### Why an on/off `last_changed` is a sound stamp

Home Assistant keeps an automation entity across a reload only when its name and `raw_config` are unchanged (`automation/util.py`: `automation_matches_config`, `_automation_matches_config`). Any other change replaces the entity (`_async_replace_automations`). A registered entity first writes `unavailable` and then its new `on`/`off`, and an unregistered one loses its state, so either way the `on`/`off` state gets a new `last_changed`. `raw_config` is set only in the entity's constructor, WS `automation/config` answers exactly it, and a blueprint instance's `raw_config` is already substituted (`automation/config.py`). A restart starts with an empty state machine, and restore keeps `last_triggered`, not `last_changed`. An unavailable entity can be replaced by another unavailable one without a new `last_changed`, so only `on` and `off` count (Astra).

### Home scope

The scope also names the CLI build (`Version`, `BuildStamp`), so a later `targetsPlace` that reads more than `target` starts from an empty index. Uninstall removes the file with the other cache files. The scope is a hash of every id the profile holds for its Relay and home: the profile id, the Relay instance id (Link pairings), the Relay's TLS pin (secure pairings), the Relay URL, and the Home Assistant URL. A profile without a Relay URL gets no index, and a file with another scope is ignored and then replaced. Home Assistant has no instance id the CLI can read, so a standalone Relay repointed to another home under the same profile and URLs keeps its scope. A wrong skip there would still need the same entity id with an on/off state of the same microsecond `last_changed`. The App's safety-check worker keeps its own files in the App.

### Replay of a cut preview

A skip is recorded as a session decision (`place-index <automation> <stamp> <places>`, `decide`), so it enters a cut plan's reads. In a replay the index file is not read: a decision the preview made at the same stamp is replayed, and anything else is read live. A config read the preview never made marks the replay diverged, and a fresh preview follows. So a changed automation, or one the preview skipped and the replay can't, never passes silently.

### Threat model

The file sits beside the plan store, the undo list, and the profile with the Relay credential. A program that can write it can already read the credential and act directly. Entries hold only entity, device, area, floor, and label ids.

### What stays the same

Named consumers (related to the entity or its device), the 100-config cap, the read deadline, the drift basis (skipped automations never entered it), the listener scan, and `quiet`.

## Proof

- Unit (`cli/tier_place_index_test.go`, fake HA):
  - a warm index skips 20 room automations and still reads, and asks for, the area-triggered unlock;
  - a changed `last_changed` reads again and asks;
  - a blueprint instance is read every time;
  - a state without `last_changed` and an unavailable automation are never indexed;
  - a malformed file and another home's file are empty;
  - a config read before the state is never paired with it;
  - a replay skips only what its preview skipped at the same stamp, and diverges otherwise.

  Mutation checks: removing the stamp comparison, the memo check, the on/off rule, or the replay key each fails a test.
- Test bed: the `calls.mjs` place rows (step h) run cold, then warm; the 300-automation measurement before and after, five runs each, cold and warm.

## Reviews

Astra (gpt-6-astra, medium) on the first draft: disagreed, with three P1s and three P2s. All are applied above:

- P1: the stamp is only sound for `on`/`off`;
- P1: an earlier config read can't be paired with a later state;
- P1: entries are scoped per home;
- P2: the blueprint premise is corrected;
- P2: replay through `decide`;
- P2: validation, concurrency, and size bounds.

Its simpler first step, one `/api/states` read for large place scans, is measured before and after on the bed.

## Not done

- A Home Assistant side surface: Core has no config revision token, and the Relay stays dumb.
- The state reads of place-only automations stay (one GET each); with a warm index they replace a GET plus a WS read each.
