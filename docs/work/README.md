# Work Docs

Temporary active planning and spec docs live here.

Rules:
- one short work doc per topic by default
- declare status: `active`, `merged`, `superseded`, or `abandoned`
- update the real SSOT in the same PR that lands the behavior
- archive or delete the work doc immediately after
- only `active` work docs may remain on `main`; CI rejects every other status
- `next-release-body.md` is the version-free draft and carries no version, by
  design: no pre-release PR may presume the number. A `<version>-release-body.md`
  must target a version newer than `package.json` — the release-prep PR renames
  the draft, then moves the consumed one to `docs/archive/work/`
