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

- Only where the owner steps would print to a person: `cloud remove` in a terminal, and setup's hold and paused-Link screens (#813), when `haSignInPossible` holds (desktop, browser, a plain local Home Assistant address; `cli/ha_signin_session.go:90-105`) and the Relay advertises sign-in.
- A two-item menu above the steps: "Clean up for me (sign in to Home Assistant once)" / "Show me the steps". Default: the first. `--yes`, `--json`, no terminal: the steps as today, never the offer.

### Steps

1. **Sign in.** The existing browser sign-in on the saved local address, never the Link address (`parseLocalHAOrigin`, `cli/ha_signin_session.go:66-81`). Its own refresh token is the one marked current; it is deleted last. Today `finishHASignIn` refuses a non-owner before its `prepare` hook (`errSignInNotOwner`, `cli/ha_signin_session.go:341-343`) and closes the connection when it returns: cleanup needs a new hook that runs steps 2–6 on the open connection and lets a non-owner through.
2. **Who.** `auth/current_user`. Not the owner → device revoke can't run (step 4 says so), the rest still runs for this user's own sign-ins.
3. **This computer's Link sign-in.** `auth/refresh_tokens` lists only the signed-in user's tokens (`RefreshTokens`, `cli/cloud_ha_api.go:76-85`). Delete exactly:
   - the stored `refresh_token_id`, when this computer has one (`OAuthSecretEnvelope`, `cli/cloud_oauth_secret_store.go:36-50`; or the revocation checkpoint), and it is in the list;
   - else every token whose `client_id` equals one of this profile's stored `oauth_client_id`s exactly (both `current` and `pending`, `cli/config_cloud.go:39-45,60-63`; the loopback address with its port), type `normal`, not the current one. With the key store lost, the stored token id is usually lost too, so this is the normal path.
   - None stored: list this user's HA NOVA sign-ins (client id ends in `/ha-nova`, validated as a loopback client id) with when each was made and last used, and ask "Delete all of these?" (default No). Yes deletes them; the line says it also turns off Link on this person's other computers, and may stop an HA NOVA setup running right now on another computer.
   - Delete with `auth/delete_refresh_token` per id, then list again: a matching token still there → stop.
4. **This computer's device.** Owner only. Today's `revoke-self` needs the device key, which is exactly what is missing (`nova/src/http/handlers/cloud.ts:113-140`), and `config.json` holds no device id for a finished Link setup (the ids at `cli/config_cloud.go:65-73` exist only during activation or after a revoke; the id lives in the lost key). So the device is found by what `config.json` does keep: the non-secret install id (`client_install_id`, `cli/config.go:21-26`, derived per profile by `pairingClientInstallID`, `cli/device_credential_pending_record.go:126`) and the Relay instance id.
   - New owner-only machine route on the App: `POST /cloud/v1/device/revoke-owner {client_install_id, relay_instance_id}` (owner check as `checkOwner`, `nova/src/security/owner-check.ts`; machine-only like the chat-app route).
   - It revokes only active records that are Link-bound (`cloudUserId` and `cloudRelayInstanceId` set, `nova/src/security/device-registry-types.ts:15-16`) with that install id and this Relay instance. A local-only record of the same install is never touched. More than one match → refused, manual.
   - Answer: `revoked` (it revoked one now), `already_revoked` (the registry holds a revoked Link record for exactly that install and Relay), or `not_found`. Only the first two count as done; `not_found` and a wrong Relay instance stop the run (no fail-open on a stale id). ❓ Check on the test bed how a profile with both a local and a Link device derives their install ids.
5. **Someone else?** If the stored `ha_user_id` is another user, or none is stored and step 3 found nothing, ask: "Did anyone else sign in to Home Assistant Link for HA NOVA on this computer?" [No, only me] [Yes, or not sure]. Anything but "No" keeps step 2 manual for them.
6. **Own token.** Delete the sign-in's own refresh token (`revokeAndVerifyCloudAuthorizationWithClient`, as setup does, `cli/ha_signin_session.go:325-328`).
7. **Local removal.** Only when steps 3 and 4 were verified (or step 4 was not needed: no device was ever bound) and step 5 answered "No": run the same local removal `--confirm-remote-access-revoked` runs, in this process, marked as verified by sign-in. Nothing new is stored in `config.json` or the checkpoint beyond what that path already stores.

### Outcomes

- All done: "Home Assistant Link is removed from this computer. HA NOVA still works here on your home network."
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

> Did anyone else sign in to Home Assistant Link for HA NOVA on this computer?
> › No, only me
>   Yes, or not sure

Not the owner:

> You're signed in as Anna, not the owner. I removed Anna's sign-in. The owner still needs to revoke this computer. Run ha-nova cloud remove again and let the owner sign in this time, or follow these steps:
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

- Go, fake Home Assistant and fake App (`cli/cloud_*_test.go`): exact id match deletes one token; exact client id match; no stored id asks and defaults to No; the current sign-in's own token is never deleted until step 6; a token still listed after delete stops before local removal; not the owner skips the device step and keeps the manual line; another stored `ha_user_id` keeps step 2 manual; any read failure removes nothing; `--yes`, `--json`, and no-terminal runs never show the offer (golden).
- TS (`tests/http/`): `revoke-owner` refuses a non-owner, a browser request, another Relay instance, and two matching records; never revokes a local-only record of the same install; `already_revoked` only for a revoked Link record of exactly that install and Relay; an unknown install id answers `not_found`.
- Mutation check: removing the re-list after delete makes a test fail.
- Disposable-HA E2E (`scripts/e2e/disposable-ha/`): a real Home Assistant with a real loopback refresh token; the run deletes it and only it. Never a live home (hard rule), and never on a macOS login keychain (Linux container lane).

## Open questions for the maintainer

1. Default menu choice: "Clean up for me" (default) or "Show me the steps"?
2. Offer it in `ha-nova uninstall` too, when Link is the blocker? Default: yes, same code.
3. The step 5 question: keep it (default), or trust the stored `ha_user_id` alone when there is one?
4. `revoke-owner` is a new App route: ship it in the 0.28 App, with the CLI falling back to manual steps on an older App (default)?
5. Home Assistant may skip the password when the browser is already signed in as the owner. Is "a person at the desktop" enough proof then (default: yes, as sign-in pairing already accepts), or should cleanup ask for a typed yes in the terminal before deleting anything?
