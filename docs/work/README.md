# Work Docs

Temporary active planning and spec docs live here.

Rules:
- one short work doc per topic by default
- declare status: `active`, `merged`, `superseded`, or `abandoned`
- update the real SSOT in the same PR that lands the behavior
- archive or delete the work doc immediately after
- only `active` work docs may remain on `main`; CI rejects every other status
- a release-body draft must target a version newer than `package.json`; move it
  to `docs/archive/work/` in the release-prep PR that bumps that version
