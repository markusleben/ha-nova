# Manager -> Transport Dependency Matrix

Which legacy MCP manager needs which transport? Critical for App Skill migration.

## Matrix

| Manager | REST | WebSocket | Filesystem | Backup | Skill-Ready? |
|---------|------|-----------|------------|--------|--------------|
| **entity_manager** | States, Services | Entity Registry, Device Registry | - | - | Yes (REST + Relay WS) |
| **automation_manager** | CRUD, Services, History | Traces, Config List | - | Optional | Yes (REST + Relay WS) |
| **script_manager** | CRUD, Services | Traces, Config | - | Optional | Yes (REST + Relay WS) |
| **helper_manager** | Services | Storage CRUD, Config Entry Flow | - | - | Yes (Relay WS) |
| **control** | Services | Entity State (optional) | - | - | Yes (REST direct) |
| **organizer_manager** | - | Area/Floor/Label/Category/Device/Entity Registry | - | - | Yes (Relay WS) |
| **dashboard_manager** | - | Lovelace Config/Save/Delete, Resources | - | - | Yes (Relay WS) |
| **energy_manager** | - | Energy Prefs, Validate, Solar, Fossil | - | - | Yes (Relay WS) |
| **integration_manager** | Config Entries, Options Flows | Config Entry Get | - | - | Yes (REST + Relay WS) |
| **system_manager** | Check Config, Logs, Services | System Health | - | - | Yes (REST + Relay WS) |
| **event_monitor** | History | Subscribe Events, Subscribe Trigger | - | - | Yes (REST + Relay WS Subscribe) |
| **scene_manager** | States, Services | - | - | - | Yes (REST direct) |
| **repairs_manager** | - | Repairs List | - | - | Yes (Relay WS) |
| **playbook_manager** | - | - | - | - | Yes (pure skill knowledge) |
| **template_manager** | Services (Reload) | - | YAML read/write | Yes | Partial (Relay Files) |
| **sensor_manager** | Services (Reload) | - | YAML read/write | - | Partial (Relay Files) |
| **file_manager** | - | - | Read/Write/List/Delete | - | No (Relay Files) |
| **backup_manager** | - | - | Backup CRUD | Yes | No (Relay Backups) |

## Summary

- **7 managers** need REST only -> App Skill can call the HA API directly
- **7 managers** need REST + WS -> App Skill + Relay WS proxy
- **4 managers** need filesystem access -> App Skill + Relay file endpoints

## Automation Manager: Detailed Dependencies

The most important manager for Phase 1. Breakdown by action:

### REST Only (Phase 1b - no WS needed)
| Action | REST Endpoint |
|--------|---------------|
| list | `GET /api/states` (filter `automation.*`) |
| get | `GET /api/config/automation/config/{id}` |
| search | `GET /api/states` + client-side filter |
| create | `POST /api/config/automation/config/{id}` |
| update | `GET` + `POST /api/config/automation/config/{id}` |
| delete | `DELETE /api/config/automation/config/{id}` |
| turn_on/off/toggle | `POST /api/services/automation/{action}` |
| enable/disable | `POST /api/services/automation/{action}` |
| reload | `POST /api/services/automation/reload` |
| trigger | `POST /api/services/automation/trigger` |
| validate_config | Local logic + `GET /api/states` + `GET /api/services` |

### Requires WS (Phase 1c)
| Action | WS Type |
|--------|---------|
| trace_summary | `trace/list` + `trace/get` |
| trace_get | `trace/get` |
| verify_trigger | `automation/trigger` + trace observation |
| diagnose | Traces + States + Services + config analysis |

### Requires Backup (Phase 2)
| Action | What |
|--------|------|
| batch_update | Backup before batch, rollback on failure |
| batch_delete | Backup before delete |
| backup/restore | Explicit backup management |

### Fully Local Logic (Skill knowledge, no API call)
| Feature | What |
|---------|------|
| Best-practice rules | 70 rules, including 2 automation-specific rules |
| Config normalization | Triggers/Conditions/Actions as arrays, ID generation |
| Dependency analysis | Load all configs + scan references |
| Diagnostic heuristics | Oscillation, writer conflicts, condition blockers |
