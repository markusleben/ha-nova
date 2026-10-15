# Setup: installed-App detection, one install link, uninstall guard — Spec (2026-10-16)

Status: active

## Problem

- Every extra computer walks the full App install (repository, store search,
  Install, Start) although the App already runs.
- The repository-only My link (`supervisor_add_addon_repository`) is a known
  upstream bug (home-assistant/my.home-assistant.io#698) and still leaves the
  store search to the user.
- `ha-nova uninstall` offered the App removal with default Yes and never
  mentioned that the App is shared by every paired computer.

## Behavior

1. **Detection.** On the host -> install transition of the pairing flow (setup
   wizard and add-server flow), the CLI calls the unauthenticated
   `GET <relay>/pair/v1/info`. Only HTTP 200 with the relay's
   `{"ok":true,"data":{"protocol_version":"v1"}}` envelope counts; anything
   else (closed port, timeout, HTML, other status, other protocol) shows the
   install steps as before. Back-navigation from pairing and the repair menu's
   full guidance always show the steps (no re-skip).
2. **Combined link.** With a Home Assistant version >= 2026.2 known from the
   mDNS TXT `version`, the wizard opens
   `<ha>/_my_redirect/supervisor_app?app=<slug>&repository_url=<repo>`: the App
   page asks once to add the missing repository, then Install + Start. Source:
   home-assistant/frontend `src/panels/my/ha-panel-my.ts` (`supervisor_app`,
   added in #29142, shipped in frontend 20260128 = HA 2026.2.0) and
   `src/panels/config/apps/ha-config-app-dashboard.ts`
   (`_handleMissingRepository`). Unknown or older version: the two-step
   repository link + store search stays. The instance-local `_my_redirect`
   is kept (no my.home-assistant.io instance-URL step).
3. **Uninstall guard.** The guided teardown offer warns that the App is shared,
   defaults to No, and then asks "Is this the last computer that uses NOVA
   with this Home Assistant?" (default No). No keeps the App, skips the
   removal steps, and the final notes say the App stays for the other
   computers. The CLI cannot list paired devices (owner-only NOVA page), so
   the user confirms.

## Limits

- The version is known only for mDNS-discovered instances; typed or saved
  addresses use the two-step fallback.
- Detection needs a relay that answers `/pair/v1/info` (secure pairing).
  Older relays show the install steps (safe default).

## Docs

`nova/DOCS.md` is served to Home Assistant straight from `main`, so it gains
the two user-facing sentences (detection skip; uninstall defaults to No and
asks about other computers) in the release-prep PR, like README edits.
