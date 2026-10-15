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
   `GET <relay>/pair/v1/info` (bounded to 5 s, with progress). Only HTTP 200
   with the relay's `{"ok":true,"data":{"protocol_version":"v1"}}` envelope
   counts; anything else (closed port, timeout, HTML, other status, other
   protocol) shows the install steps as before. Only the relay address derived
   from the chosen Home Assistant is probed: `--relay-url` or a kept custom
   relay address always shows the steps. On a skip, the pairing step shows
   "NOVA Relay is already running in Home Assistant. Skipping the install
   step." plus an update hint when `relay_version` is below
   `min_relay_version`; back from pairing returns to the address step. The
   repair menu's full guidance always shows the steps.
2. **One install link.** The wizard opens
   `<ha>/_my_redirect/supervisor_addon?addon=<slug>&repository_url=<repo>`:
   the App page asks once to add the missing repository, then Install + Start.
   The redirect has taken `repository_url` on every supported version
   (home-assistant/frontend `hassio/src/hassio-my-redirect.ts` +
   `hassio/src/addon-view/hassio-addon-dashboard.ts` before 2026.2;
   `src/panels/my/ha-panel-my.ts` + `src/panels/config/apps/ha-config-app-dashboard.ts`
   since), so no version gate. The instance-local `_my_redirect` is kept (no
   my.home-assistant.io instance-URL step).
3. **Uninstall guard.** The guided teardown warns that the App is shared and
   first asks "Is this the last computer using HA NOVA with this Home
   Assistant?" (default No). Yes then offers the walkthrough (default No; No
   prints the removal checklist). The removal checklist appears ONLY after
   that explicit yes: every other path (No, back, Home Assistant unreachable,
   no saved address, `--yes`, non-interactive, Windows recovery, the Windows
   background helper without `--last-computer-confirmed`) ends with one plain
   note that the App was left as it is and should be removed later only if no
   other computer uses HA NOVA. The CLI cannot list paired devices (owner-only
   NOVA page), so the user confirms.

## Limits

- Detection needs a relay that answers `/pair/v1/info` (secure pairing).
  Older relays show the install steps (safe default).
- A standalone Container relay on the HA host also answers; the wizard then
  goes to pairing, where 'manual' keeps the token path.
- `--yes` and other non-interactive uninstalls on the last computer get no
  App removal steps; the user removes the App in Home Assistant by hand.

## Docs

`nova/DOCS.md` is served to Home Assistant straight from `main`, so it gains
the two user-facing sentences (detection skip; uninstall keeps the App unless
this is the last computer) in the release-prep PR, like README edits.
