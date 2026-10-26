# "Clean up for me" for a stale Home Assistant Link sign-in (2026-10-24)

Status: active

Target: 0.28. Follow-up to #813 (its known limits: "A one-click 'clean up for me' … is out of scope; planned for 0.28").

## Problem

- When `ha-nova cloud remove` can't check this computer's Home Assistant Link sign-in or its NOVA device (the key is gone, or the sign-in never finished), it stops and prints three owner steps (`cloudAuthorizationRecoverySteps`, `cli/cloud_manual_revocation_recovery.go:120-151`):
  1. as the owner, revoke this computer on the NOVA App page;
  2. each person who uses HA NOVA deletes the HA NOVA entries under Profile → Security → Refresh tokens;
  3. run `ha-nova cloud remove --server <p> --yes --confirm-remote-access-revoked <p>`.
- The same steps show wherever the safety hold shows (`renderCloudSecurityHold`, `cli/cloud_security_hold_ux.go:56-82`). For most users that's a wall of clicks in two places plus a long command. Most stale sign-ins belong to the owner, on this one computer.

## Goal

- One choice, "Clean up for me": the owner signs in to Home Assistant once in the browser; HA NOVA deletes this computer's Link sign-in, revokes this computer's NOVA device, checks both, and then does the same local removal the `--confirm` command does.
- Only sign-ins by other people stay manual, with the same step 2 for them.
- Every step fails closed: if HA NOVA can't prove a step worked, nothing local is removed and the manual steps show, as today.

## Non-goals

- Deleting other people's sign-ins (Home Assistant only lets a user delete their own).
- Agents: never offered without a person at the desktop, never in output an AI assistant reads.
- Named profiles beyond what `cloud remove` already supports, SSH sessions, and computers without a browser (they keep the manual steps).
- Changing the safety hold itself or what `cloud remove` refuses.

## Design

### When it's offered

- Only where the owner steps would print to a person: `cloud remove` and `ha-nova uninstall` in a terminal (when Link is what blocks it), and setup's hold and paused-Link screens (#813) and its leftover-Link choice (Remove there runs `cloud remove` in the same process, with the person's choice as its confirmation, not `--yes`; `docs/work/2026-10-26-link-leftover-setup-doctor-spec.md`), when `haSignInPossible` holds (desktop, browser, a plain local Home Assistant address; `cli/ha_signin_session.go:90-105`) and the Relay advertises sign-in.
- A two-item menu above the steps: "Clean up for me (sign in to Home Assistant once)" / "Show me the steps". Default: the first. `--yes`, `--json`, no terminal: the steps as today, never the offer.

### Steps

1. **Sign in.** The existing browser sign-in on the saved local address, never the Link address (`parseLocalHAOrigin`, `cli/ha_signin_session.go:66-81`). Its own refresh token is the one marked current; it is deleted last. Today `finishHASignIn` refuses a non-owner before its `prepare` hook (`errSignInNotOwner`, `cli/ha_signin_session.go:341-343`) and closes the connection when it returns: cleanup needs a new hook that runs steps 2–6 on the open connection and lets a non-owner through.
2. **Who.** `auth/current_user`. Not the owner → device revoke can't run (step 4 says so), the rest still runs for this user's own sign-ins.
3. **This computer's Link sign-in.** `auth/refresh_tokens` lists only the signed-in user's tokens (`RefreshTokens`, `cli/cloud_ha_api.go:76-85`). Delete exactly:
   - **Every slot, each on its own.** A profile can hold up to three Link sign-ins at once: current, pending, and retiring (an interrupted reconfiguration; `cloudAuthorizationCleanupPlan.envelopes()`, `cli/cloud_authorization_cleanup_plan.go:226-238`). Each slot is matched, deleted, and verified separately; finding one never skips another.
   - Per slot: its stored `refresh_token_id` (`OAuthSecretEnvelope`, `cli/cloud_oauth_secret_store.go:36-50`, or the revocation checkpoint's slot), **and** every token whose `client_id` equals that slot's client id exactly (the envelope's or checkpoint's `client_id`, else `config.json`'s `current`/`pending` `oauth_client_id`, `cli/config_cloud.go:39-45,60-63`; the loopback address with its port), type `normal`, not the sign-in's own. Using both covers a slot whose token was refreshed or re-issued. With the key store lost, the stored ids are usually lost too, so the client-id match is the normal path. Client ids carry a random loopback port (`cli/cloud_oauth_loopback.go:101`), so another computer can collide: more than one token on one slot's client id → the "Delete all of these?" list instead.
   - None stored: list this user's HA NOVA sign-ins (client id ends in `/ha-nova`, validated as a loopback client id) with when each was made and last used, and ask "Delete all of these?" (default No). Yes deletes them; the line says it also turns off Link on this person's other computers, and may stop an HA NOVA setup running right now on another computer.
   - Delete with `auth/delete_refresh_token` per id, then list again: a token matching any slot still there → stop.
4. **This computer's device.** Owner only. Today's `revoke-self` needs the device key, which is exactly what is missing (`nova/src/http/handlers/cloud.ts:113-140`), and `config.json` holds no device id for a finished Link setup (the ids at `cli/config_cloud.go:65-73` exist only during activation or after a revoke; the id lives in the lost key). So the device is found by what `config.json` does keep: the non-secret install id (`client_install_id`, `cli/config.go:21-26`, derived per profile by `pairingClientInstallID`, `cli/device_credential_pending_record.go:126`) and the Relay instance id.
   - New owner-only machine route on the App: `POST /cloud/v1/device/revoke-owner {client_install_id, relay_instance_id}` (owner check as `checkOwner`, `nova/src/security/owner-check.ts`; machine-only like the chat-app route).
   - It revokes only active records that are Link-bound (`cloudUserId` and `cloudRelayInstanceId` set, `nova/src/security/device-registry-types.ts:15-16`) with that install id and this Relay instance. A local-only record of the same install is never touched. More than one match → refused, manual.
   - Answer: `revoked` (it revoked one now), `already_revoked` (no active match, and the registry holds a revoked Link record for exactly that install and Relay), or `not_found`. An active match always wins over a revoked one. Only the first two count as done; `not_found` and a wrong Relay instance stop the run (no fail-open on a stale id). A local and a Link device of one profile share the install id, and activating one drops the other (`nova/src/security/device-registry-mutations.ts:180`), so only one is ever active; the Link-bound filter is a safety net.
5. **Someone else?** If the stored `ha_user_id` is another user, or none is stored and step 3 found nothing, ask: "Did anyone else use HA NOVA on this computer with their own Home Assistant login?" [No, only me] [Yes, or not sure]. Anything but "No" keeps step 2 manual for them. "No" on the broad delete (step 3) shows the steps too.
6. **Own token.** Delete the sign-in's own refresh token (`revokeAndVerifyCloudAuthorizationWithClient`, as setup does, `cli/ha_signin_session.go:325-328`).
7. **Local removal.** Only when steps 3 and 4 were verified (or step 4 was not needed: no device was ever bound) and step 5 answered "No": run the same local removal `--confirm-remote-access-revoked` runs, in this process, marked as verified by sign-in. Nothing new is stored in `config.json` or the checkpoint beyond what that path already stores.

### Outcomes

- All done, `cloud remove` with a home-network connection: "Home Assistant Link is removed from this computer. HA NOVA still works here on your home network."
- All done, Link-only setup (`cli/command_cloud_remove.go:62-71` revokes the device): "Home Assistant Link is removed from this computer. To use HA NOVA here again, run ha-nova setup."
- All done, `uninstall`: uninstall's own outcome, after one line "Removed this computer's Home Assistant Link sign-in."
- Older App (no `revoke-owner`): the menu isn't shown; one line says "Update the NOVA App to let HA NOVA do this for you.", then the steps.
- Partly done (e.g. not the owner, or another person's sign-in): what was done, then only the remaining manual steps, then the `cloud remove` line as today.
- Anything failed or unread: "Nothing was removed from this computer yet." plus the full manual steps.

## Security

- **Only own tokens.** Home Assistant's own API limits deletion to the signed-in user, so a mistake can't touch another person's sign-in.
- **Exact match first.** Stored token id, then the exact stored client id; the broad "all HA NOVA sign-ins" delete only after the person sees the list and says yes.
- **Proof, not trust.** Local removal waits for a re-read that shows the tokens gone and the device revoked. A delete that answered OK but still lists the token stops the run.
- **The device revoke is owner-only** on the App side, checked fresh per request, and refuses anything but a cloud-bound device of this Relay instance; it never revokes local devices.
- **No agent path.** The offer needs a terminal and a desktop browser; output an AI assistant may read never names the offer or the `--confirm` command (#813's rule stays).
- **Tokens stay in memory.** As in sign-in pairing: the access token is never stored, and the sign-in's refresh token is deleted at the end, also on failure (deferred).
- **Home Assistant Link is not used** for any of this; everything goes to the saved local address.

## UX copy (draft)

Menu:

> Home Assistant Link isn't fully set up on this computer, and HA NOVA can't check it from here.
> › Clean up for me (sign in to Home Assistant once)
>   Show me the steps

While running:

> Sign in to Home Assistant as the owner. Your browser opens now.
> ✓ Removed this computer's Home Assistant Link sign-in.
> ✓ Revoked this computer on the NOVA App page.

Question (step 5):

> Did anyone else use HA NOVA on this computer with their own Home Assistant login?
> › No, only me
>   Yes, or not sure

Not the owner:

> You're signed in as Anna, not the owner. I removed Anna's sign-in. The owner still needs to revoke this computer. Run this again and let the owner sign in this time, or follow these steps:
> 1. Open the NOVA App page as the owner: <link>
> 2. If "Markus-MacBook" is listed with a "cloud" tag, click Revoke.
> 3. Run: ha-nova cloud remove --server default --yes --confirm-remote-access-revoked default

Broad delete:

> I found 3 HA NOVA sign-ins for Markus but can't tell which one is this computer's:
> • made 12 Oct, last used 20 Oct
> • made 3 Oct, last used today
> • made 1 Sep, never used
> Delete all of them? Home Assistant Link then stops on your other computers too. Their home-network connection keeps working. If you're setting up HA NOVA on another computer right now, finish that first.

## Test plan

- Go, fake Home Assistant and fake App (`cli/cloud_*_test.go`): exact id match deletes one token; current, pending, and retiring slots each deleted and verified (a stored id in one slot doesn't skip the client-id match of another); exact client id match; no stored id asks and defaults to No; the current sign-in's own token is never deleted until step 6; a token still listed after delete stops before local removal; not the owner skips the device step and keeps the manual line; two tokens on one slot's client id ask; `uninstall` and Link-only outcomes (golden); an older App shows the update line; another stored `ha_user_id` keeps step 2 manual; any read failure removes nothing; `--yes`, `--json`, and no-terminal runs never show the offer (golden).
- TS (`tests/http/`): `revoke-owner` refuses a non-owner, a browser request, another Relay instance, and two matching records; never revokes a local-only record of the same install; `already_revoked` only for a revoked Link record of exactly that install and Relay; an unknown install id answers `not_found`.
- Mutation check: removing the re-list after delete makes a test fail.
- Disposable-HA E2E (`scripts/e2e/disposable-ha/`): a real Home Assistant with a real loopback refresh token; the run deletes it and only it. Never a live home (hard rule), and never on a macOS login keychain (Linux container lane).

## Decisions (maintainer, 2026-10-24)

1. Default menu choice: "Clean up for me".
2. Offered in `ha-nova uninstall` too, same code.
3. Keep the step 5 question.
4. `revoke-owner` ships in the 0.28 App; on an older App the CLI falls back to "Show me the steps".
5. A person at the desktop is enough proof (no extra typed yes). Known limit: with trusted-networks sign-in Home Assistant may ask nothing, so an agent with a terminal could accept the default; the damage stays on this computer, and the broad delete defaults to No.
