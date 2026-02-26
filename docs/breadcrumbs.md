# Breadcrumbs

## 2026-02-25
- Vollständige Bestandsaufnahme der vorhandenen Doku in `ha-nova` abgeschlossen.
- Zielbild bestätigt: Thin Bridge (dumm) + Skills (schlau), kein Business-Logic-Drift in Server.
- Brainstorming-Session mit 10 Klärungsfragen abgeschlossen.
- Umsetzungsplan für Phase 1a erstellt:
  - `docs/plans/2026-02-25-phase-1a-foundation-design.md`
- Plan nachgeschärft: LLT (`HA_TOKEN`) im Onboarding als Pflichtentscheidung verankert.
- Ausführung gestartet auf Branch `feat/phase-1a-foundation` (Worktree wegen fehlendem Initial-Commit nicht möglich).
- Remote-Setup abgeschlossen: `origin` -> `https://github.com/markusleben/ha-nova.git`, Branch `feat/phase-1a-foundation` gepusht.
- Batch 2 (Tasks 4-6) umgesetzt: WS-Client, Health-Handler, WS-Allowlist inkl. Tests und Verifikation.
- Batch 3 (Tasks 7-9) umgesetzt: `/ws`-Handler, Phase-1a-Skills, Acceptance-Matrix mit Curl-Smoke-Evidenz.
- PR #1 (`feat/phase-1a-foundation` -> `main`) erstellt und direkt gemerged.
- Post-Merge-Codex-Review ausgeführt; Wiring-Lücke für `/health` + `/ws` identifiziert und in `main` gefixt.
- Follow-up Review auf Fix-Commit ohne Findings; verbleibendes Risiko nur für unbekannte alte `createApp()`-Callsites ohne Optionen.
- Striktes Code-Review für `2e6d924..f640807`: keine kritischen Bugs, aber Health/WS-Handler noch nicht im Router registriert, daher Integrationstoff fehlt.
- Phase 1b gestartet auf Branch `feat/phase-1b-rest-skills`.
- Phase-1b-Design und Deliverables erstellt:
  - `docs/plans/2026-02-25-phase-1b-rest-skills-design.md`
  - `skills/ha-entities.md`
  - `skills/ha-control.md`
  - `skills/ha-automation-crud.md`
  - `skills/ha-automation-control.md`
  - `docs/phase-1b-acceptance.md`
- Konsistenzcheck (`ha-nova` Routing vs Skill-Dateien) und Vollverifikation (`test`, `typecheck`, `build`) erfolgreich.
