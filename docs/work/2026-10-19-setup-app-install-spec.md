# Setup: install the NOVA App after sign-in — Spec (2026-10-19)

Status: active

Part of the 0.27 onboarding train (PR A3, text path first). The browser setup page (A6) reuses this engine.

## Problem

On HA OS and Supervised, every first computer leaves the wizard for three manual clicks in Home Assistant: Add repository, Install, Start. The HA sign-in that setup already does for pairing is an owner session that can do these steps itself.

## Contract

**Consent.**
- The App step offers "Sign in and install NOVA" or, for a running App below `min_relay_version`, "Sign in and update NOVA". The alternatives are "Install it myself in the browser" or "Update later".
- Only that explicit choice starts anything. In the typed fallback, Enter alone is not consent: the user types `yes`.
- The offer needs all of these:
  - a pairing step follows;
  - a desktop browser sign-in is possible (TTY in and out, no SSH, a display on Linux, a local HA address);
  - the derived relay address (never `--relay-url` or a kept custom address);
  - the official App identity (not developer builds).
- A running App that is current gets no offer.

**One sign-in.**
- The install runs inside the owner's HA sign-in, after the owner check and before the ingress session (`finishHASignIn` prepare hook).
- The resulting ingress session pairs this computer: there is no second sign-in.
- Tokens stay in the CLI process. The refresh token is revoked on every path, as for sign-in pairing.
- Budgets:
  - install ≤ 10 min;
  - App readiness ≤ 2 min;
  - the other steps 30 s each.

**Allow-list** (`cli/ha_supervisor_app.go`). WS `supervisor/api` gets only these calls; anything else is refused before it is sent:
- `GET /store/repositories`;
- `POST /store/repositories` with body exactly `{"repository": "https://github.com/markusleben/ha-nova"}`;
- `POST /store/reload`, no body;
- `GET /addons/<slug>/info`;
- `GET /store/addons/<slug>`;
- `POST /store/addons/<slug>/install|update` with body exactly `{"background": true}`;
- `POST /addons/<slug>/start`;
- `GET /jobs/<32 hex>`.

Plus the read `get_config` (`hassio` component = Supervisor present). `<slug>` is derived from the pinned URL as Supervisor does (`sha1(lower(url))[:8]` + `_ha_nova_relay` = `2368fcfa_ha_nova_relay`).

**Trust.**
- A listed repository counts only with source equal to the pinned URL and the derived slug. Lookalikes are refused: "Home Assistant lists a different NOVA source. Nothing was installed."
- The App must have:
  - that slug;
  - that repository;
  - the name `NOVA Relay`;
  - plain version strings.
- Only prebuilt images are accepted: `build: false` on the App and on the update target.
- An update consent never installs.
- After the change, the version must reach `min_relay_version`.

**Failures** never leave silent half-state, and nothing is ever uninstalled.
- Each failure prints one line, then what Home Assistant holds now.
- A write without a confirmed outcome reads "may still be …". Supervisor shields add, start, and background jobs.
- Then it names the next step:
  - when the App runs, pairing continues;
  - when a change is still in flight, the App page with "wait, then click Start";
  - otherwise, today's App-page link.
- Container/Core, or a system that can't run the App, gets the container guide and no App-page steps.

## Tests

- `cli/ha_supervisor_app_test.go`: allow-list, trust, update rules, unknown outcomes.
- `cli/setup_app_install_test.go`: consent, one sign-in, fallbacks.
- `cli/setup_signin_ha_e2e_test.go` with `HA_NOVA_SIGNIN_E2E_INSTALL=1`: real throwaway HA OS from a Linux container.
