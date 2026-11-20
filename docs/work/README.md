# Work Docs

Planning and spec docs for work in progress, and the specs of work that shipped.

Rules:
- one short work doc per topic by default
- declare status: `active` while in progress, `shipped` (optionally with the
  version) once the behavior is released; CI rejects every other status
- update the real SSOT in the same PR that lands the behavior; a shipped work
  doc records why, the reference docs say what is true now
- move superseded or abandoned work docs to `docs/archive/work/`
- `next-release-body.md` is the version-free draft and carries no version, by
  design: no pre-release PR may presume the number. A `<version>-release-body.md`
  must target a version newer than `package.json` — the release-prep PR renames
  the draft, then moves the consumed one to `docs/archive/work/`
