Status: active

# Next Skill Gap Implementation Spec

Date: 2026-04-03

## Goal

Implement the next dedicated HA NOVA skill surfaces that were previously still owned by `ha-nova:fallback`:

1. `ha-nova:dashboard`
2. `ha-nova:organize`
3. `ha-nova:history`

## Scope

- add repo skill entrypoints under `skills/`
- update the context-skill dispatch contract
- shrink fallback ownership to the remaining unsupported / relay-ready surfaces
- extend `dashboard` to the storage-dashboard lifecycle: create, metadata update, config update, delete
- extend `organize` to own category CRUD plus entity category assignment/removal by scope
- update active end-user/reference docs
- update installer/skill-tree contracts that assume the old fixed skill list

## Non-Goals

- no new Relay endpoints
- no server business logic
- no live dashboard/registry/history runtime implementation code
- no expansion of helper ownership in this slice

## Acceptance

- the 3 new skills exist with English-only contracts
- `skills/ha-nova/SKILL.md` routes matching intents to the new skills
- `skills/fallback/SKILL.md` no longer claims ownership of the promoted surfaces
- `dashboard` only writes/deletes dashboards whose `mode` is `storage`, resolved from `lovelace/dashboards/list`
- `dashboard` uses `lovelace/dashboards/create|update|delete` for lifecycle actions and never treats `lovelace/config/delete` as dashboard delete
- `organize` owns `config/category_registry/*` and entity category assignment/removal through `config/entity_registry/update`
- new destructive dashboard/category paths reuse exact `confirm:<token>` confirmation
- README + skill architecture reflect the new inventory
- installer/docs/tests that assume the old skill count or old flat skill names are updated
