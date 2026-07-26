# Home Assistant Cloud Remote Transport

Status: active

## Goal

Add an opt-in Home Assistant Cloud transport for HA OS/Supervised installations
without weakening the existing local transport. A configured desktop client can
use the local SPKI-pinned Relay when reachable and the canonical Nabu Casa
remote origin otherwise.

## Non-goals

- No HA NOVA-operated tunnel, hosted broker, or long-lived public endpoint.
- No remote support for Home Assistant Container or Core.
- No OAuth token, Ingress cookie, or HA credential in config, environment,
  command arguments, logs, or AI-visible output.
- No file-backed fallback for Cloud credentials.
- No automatic retry after a functional request may have reached the Relay.
- No partial endpoint rollout presented as a supported Cloud connection.

## Security contract

- OAuth refresh tokens live only in a dedicated OS keyring, never the
  file-capable Relay/device secret router.
- OAuth, WebSocket, Supervisor, and Ingress machine requests never redirect.
- Custom domains are discovery aliases; credential requests use only the
  verified canonical `https://*.ui.nabu.casa` origin.
- Functional Ingress requires a genuine Supervisor peer, exactly one HA user
  header, and an active NOVA device bound to that user and Relay instance.
- Legacy Relay tokens are never accepted through Ingress.
- A functional mutation is sent through exactly one transport. Ambiguous
  completion returns `OUTCOME_UNKNOWN`; it is never retried through another
  transport.
- Setup and explicit unlock commands may request bounded native OS-keyring
  prompts for the exact selected device and OAuth slots. Normal AI/Relay calls
  never prompt or wait indefinitely.

## Transport

The CLI uses Home Assistant's supported interfaces:

1. OAuth against the canonical Home Assistant Cloud origin.
2. Home Assistant WebSocket authentication with the short-lived access token.
3. User/Cloud/App discovery and Ingress-session creation over that socket.
4. A process-local Ingress session transports one or more bounded Relay calls.
5. The Relay separately authenticates the bound per-device credential.

The access token authenticates only WebSocket; the refresh token stays in the
keyring. Ingress cookie/path stay in memory. Only the device bearer reaches Relay.

Every redirect policy is explicit:

- OAuth authorization may follow browser navigation.
- Token, revoke, WebSocket, Supervisor, and Ingress machine clients reject
  redirects.
- Custom domains are accepted only as discovery aliases after their DNS CNAME
  resolution ends at one canonical `*.ui.nabu.casa` origin and one
  publicly-trusted TLS certificate covers both names. Credential-bearing
  requests use only that canonical origin.

## Relay instance identity

Each App installation owns a random, persistent `relay_instance_id`. It is
returned by Cloud capability discovery and included in device bindings.

- Restart/update preserves the ID.
- App reinstall creates a new ID.
- The CLI refuses to bind an existing local credential unless the local and
  Cloud IDs match.
- A stored Cloud profile whose ID changes requires explicit re-pairing.

An intentional App reinstall therefore uses an explicit recovery sequence:

- Cloud-only profile: `ha-nova cloud remove --yes --server <name>`, then
  `ha-nova cloud add --server <name> --url https://<cloud-host>`.
- Hybrid/local-capable profile: remove Cloud access, pair the local device
  again against the reinstalled App, then add Cloud access again.

An unexpected mismatch is a security stop. The user must not clear or rebind
the identity until the server change is understood.

## Ingress routes

Ingress UI routes keep their existing browser-owner contract. Machine routes
use a separate Cloud principal contract.

```
GET  /cloud/v1/info
POST /cloud/v1/device/bind
POST /cloud/v1/device/revoke-self
GET  /pair/v2/info
POST /pair/v2/start
POST /pair/v2/finish
POST /cloud/v1/device/activate

GET  /health
POST /ws
POST /core
POST /files
POST /backups
```

Every machine route requires:

- the exact Supervisor Ingress socket peer;
- exactly one non-empty `X-Remote-User-Id`;
- for functional routes, an active device credential whose stored
  `home_assistant_user_id` equals that header and whose
  `relay_instance_id` equals the current Relay instance.

`/cloud/v1/device/bind` converts an existing active local credential to the
same user-bound record only after the instance ID matches. Pairing v2 performs
the existing OPAQUE exchange entirely through Ingress and creates a pending
user-bound credential. Activation promotes only that pending credential.
Legacy shared Relay tokens are rejected on every Ingress machine route.

The failure surface is deliberately generic. A caller must not be able to
distinguish a missing user, another user's device, a stale instance, or a wrong
secret.

## OAuth

The OAuth client ID is a path on the already-bound loopback listener, for
example `http://127.0.0.1:<random-port>/ha-nova`. The callback uses the same
scheme, host, and port at a separate exact path. This follows Home Assistant's
IndieAuth redirect rule without depending on a hosted client-metadata page.
The local callback listener:

- binds `127.0.0.1` before opening the browser;
- uses a random high port and a 256-bit state value;
- accepts one `GET` request on the exact callback path;
- rejects duplicate OAuth parameters, unexpected host/path/method, missing or
  mismatched state, and oversized input;
- expires after ten minutes;
- sends a static response with `Cache-Control: no-store`,
  `Referrer-Policy: no-referrer`, and `Content-Security-Policy:
  default-src 'none'`.

The callback does not use PKCE unless Home Assistant documents and supports it
for this client type.

Immediately before the browser opens, the CLI persists the non-secret pending
OAuth metadata, snapshots the full config generation, and releases the global
client-mutation lock. After the callback it reacquires the lock and requires an
exact config match before exchanging the authorization code. Any intervening
profile, sibling, or default-selection change discards the unexchanged code and
stops the flow.

After token exchange the CLI verifies:

- `auth/current_user` returns the intended Home Assistant user;
- the refresh-token record belongs to this OAuth client and is a normal,
  expiring token;
- Cloud remote access is active;
- the expected Supervisor App and Ingress route exist.

Removal calls OAuth revoke and then verifies that refreshing the revoked token
returns `invalid_grant`. A successful HTTP status alone is not proof because the
revoke endpoint is intentionally idempotent. Reconnect rollback, ambiguous
authorization cleanup, and retirement of the previous generation all retain
the global client-mutation lock through this remote proof and the following
local cleanup. Each path then bypasses its access-session cache, reloads the
complete native envelope, and asks the native backend to compare and delete the
same encoded value in one backend operation. Ambiguous pending-grant cleanup
also binds the canonical Home Assistant origin beside generation, client ID,
and refresh token. A concurrently replaced or cross-origin envelope is
preserved as `SECRET_CONFLICT`; successful or already absent deletion
invalidates the access-session cache.

If an authorization-code exchange may have created a refresh-token session but
its response is lost, redirects unexpectedly, returns a server error, or is
malformed, setup returns `OAUTH_OUTCOME_UNKNOWN`. Redirects are never followed.
The CLI does not retry automatically; the user reviews HA NOVA sessions in Home
Assistant before starting another authorization.

## Secret storage

Cloud secrets use a separate `OAuthSecretStore` keyed by profile and generation.
Production backends are OS-only: macOS Keychain, Windows Credential Manager,
and Linux Secret Service in a validated desktop session. macOS keeps the native
caller-scoped ACL; HA NOVA never installs a trust-all application list.

Non-cancellable macOS and Windows calls run in a short-lived copy of the same
executable over bounded anonymous pipes. Secrets never enter arguments,
environment variables, files, or logs. A read timeout is `TIMEOUT`; an
ambiguous write/delete is `SECRET_OUTCOME_UNKNOWN`, preserving durable state.

After an ambiguous native mutation returns, and after any worker has terminated,
HA NOVA performs one fresh, independently bounded no-UI read. Reconciliation
does not inherit the caller's cancellation or deadline; it carries only the
holder for the already-authorized secret-access session into a new bounded
context. An exact written value or confirmed absence after delete completes the
idempotent local step. A different stored value is `SECRET_CONFLICT`. Missing
after write, unreadable, or still present after delete remains
`SECRET_OUTCOME_UNKNOWN`; none is reported as a successful mutation.

The worker is not a public keyring oracle. macOS requires matching,
non-debugged hardened-runtime processes with library validation and the same
kernel-reported Code Directory hash. Windows verifies the immediate parent
image as defense in depth within the same-user Credential Manager boundary.
The bounded schema permits only fixed/scoped OAuth slots and validated
per-profile device slots. Linux remains an in-process, context-cancellable
D-Bus implementation. Exact deletes serialize supported in-process Linux
writers; the global client-mutation lock serializes all supported HA NOVA
writers across processes. Direct same-user modification through unrelated
credential-store tools is outside the supported writer boundary and still
fails closed when its value differs.

`allow_ui=false` is mandatory for Relay, skill, doctor, status, and background
paths. Locked, missing, session-mismatched, root/sudo, headless, SSH, WSL,
container, and service contexts fail fast with a typed instruction to run
`ha-nova cloud unlock --server <name>` interactively. No code path asks for the
keyring master password.

The versioned envelope binds:

- profile ID;
- credential generation;
- canonical origin;
- OAuth client ID;
- Home Assistant user ID;
- refresh token.

Only setup, reconnect, and unlock may allow UI. They select and validate the
actual current/resumable slots before mutation. Test stores are injected only
in tests and cannot be selected by production environment variables.

Cloud Beta stays blocked until macOS artifacts have a stable signing identity
and real update/reinstall tests prove selected-slot unlock plus no-UI reuse.
Broadening the Keychain ACL is forbidden. Hardened ad-hoc development signing
enables testing but is not release evidence.

## Durable state

Configuration schema v3 gives every server profile a stable `profile_id` and
adds non-secret fields:

```
relay_instance_id
route_policy        // automatic | local | cloud
cloud.state
cloud.current       // origin, canonical origin, OAuth client ID, generation, HA user ID
cloud.pending       // same non-secret metadata for an in-progress generation
cloud.authorization_revocation_completed
                    // exact token-digest-bound slots after remote revoke
cloud.recovery_hold // fixed problem code + remediation only; never error detail or a secret
```

Cloud fields are not copied into the legacy flat-profile mirror. Saving a
profile preserves unknown fields in that profile and every sibling profile.

Credential rotation is transactional:

```
none
  -> authorizing
  -> token_stored
  -> cloud_verified
  -> device_bound_or_paired
  -> committed
  -> retiring_previous
  -> ready
```

The current generation remains usable until the pending generation has passed
all remote checks. An interrupted reconnect resumes or quarantines the pending
generation; it never destroys the working current generation first.
A reconnect authenticated as a different Home Assistant user enters the durable
`rolling_back` state before revoking and deleting the new pending grant. A crash
in that rollback resumes cleanup on the next reconnect while preserving the
previous current generation.

An uncertain secret write or remote outcome, and a security-relevant identity
or authorization stop after a durable checkpoint exists, saves a recovery hold
from status, unlock, setup, and resume against the exact raw profile loaded
with the verified runtime config. Checkpointing waits bounded for the mutation
lock, then rejects any changed snapshot. The hold survives restarts, preserves
any usable current generation, and suppresses mutating recovery guidance. A
verified interactive secure-storage check clears its verification hold only
after successful health; a later security stop atomically replaces it. Other
holds require verified `cloud remove`; strict loading rejects invalid fields.
Every raw recovery, device-revocation, authorization-revocation, and structural
profile update uses a crash-recoverable full-file transaction. The replacement,
transaction record, and prior generation are file-synced; the atomic platform
replacement and parent-directory metadata are durably committed before any
secret deletion may follow. The transaction compares the exact generation
captured by the atomic replacement, not a separate read before rename, and
reports success only while the replacement generation is still the target.
It persists a dedicated conflict-restore phase before moving any generation,
restores a racing generation without data loss, preserves all generations on
an ambiguous conflict, and recovers before the next CLI dispatch. Immediately
before committing, it runs one final target-generation hash check. The active
transaction marker then atomically retires to a durable non-secret committed
tombstone before auxiliary transaction files are garbage-collected, so a crash
never turns an incomplete transaction into an unrecorded success. Recovery
checks both active markers and committed tombstones before every CLI dispatch,
then finishes that garbage collection before durably removing the tombstone. Windows
replacements also flush both the committed target and durable prior generation
explicitly. A selected profile, sibling profile, or `default_server` change
therefore cannot be overwritten by a stale HA NOVA writer. An unsupported
external editor that ignores the client-mutation lock is detected at each
generation check but cannot participate in a cross-file atomic transaction.

## Routing

`route_policy=automatic` performs a short, authenticated, side-effect-free local
preflight. Cloud is selected only when that preflight ends in a pure network
error before the functional request is created or written.

- Authentication, authorization, TLS-pin, protocol, or version errors do not
  fall back.
- Local 401/403 remediation stays executable for the selected profile:
  `setup` for the default profile and exact `pair --server ... --relay-url ...`
  guidance using the safely quoted saved local Relay URL for a named profile.
  The same helper covers automatic preflight, direct local functional
  responses, and doctor health/WebSocket diagnostics.
- A functional request is built and sent through exactly one selected
  transport.
- Any error after dispatch may mean the operation completed and returns the
  stable `OUTCOME_UNKNOWN` classification.
- No mutating or read request is replayed through the other transport.
- `ha-nova relay --via local|cloud` overrides selection for diagnosis.
- `ha-nova server route automatic|local|cloud` persists the policy.

Once a durable device- or authorization-revocation checkpoint exists, the
profile is cleanup-only. Cloud-only readiness, explicit Cloud routing,
automatic Cloud fallback, the direct Cloud resolver, and runtime OAuth/device
access all stop before a functional Cloud network or secure-storage call. An
`automatic` profile still performs its authenticated local preflight and may
keep using a healthy local Relay. Only a pure local network failure reaches
the cleanup gate, which blocks Cloud fallback.

An authorization-revocation checkpoint may use the checkpoint's verified
cleanup canary without touching the selected production OAuth slot. A
device-only checkpoint does not prove OAuth cleanup readiness: recovery
preflights the exact selected OAuth slot and keeps it intact while completing
only the checkpointed device revocation.

The Cloud Ingress session is process-local, reusable only inside one CLI
process, bounded by expiry, and never written to disk. If Home Assistant cannot
provide a bounded lifecycle for repeated Ingress sessions, the beta remains
blocked rather than adding a persistent session broker.

## CLI and wizard

```
ha-nova cloud add [--server <name>] [--url https://<cloud-host>]
ha-nova cloud status [--server <name>]
ha-nova cloud unlock [--server <name>]
ha-nova cloud reconnect [--server <name>] [--url https://<cloud-host>]
ha-nova cloud remove [--server <name>] [--yes]
  [--confirm-remote-access-revoked <name>]
ha-nova server route <automatic|local|cloud> [--server <name>]
ha-nova relay ... --via <local|cloud>
```

`cloud status --json` always emits one JSON object, including locked storage,
unreachable Cloud, incomplete setup, and not-configured outcomes. Typed
`verification_error` and `next_command` fields let headless callers recover
without parsing human text. A named Cloud-only profile may run
`ha-nova setup --server <name>` to resume Cloud onboarding and install client
skills; named local/token/service onboarding remains unavailable and uses
`pair --server`.

If a potentially issued authorization no longer has consistent native
credentials, automatic cleanup fails closed. Recovery first requires a Home
Assistant Owner to revoke this computer under NOVA Devices and revoke HA NOVA
sessions for every user. Only then may the user run the exact profile-bound
command printed by the CLI:

```
ha-nova cloud remove --server <name> --yes \
  --confirm-remote-access-revoked <name>
```

The confirmation must exactly match the resolved profile and is rejected when
automatic cleanup remains possible. It never bypasses schema, profile,
keyring-read, corruption, lock, timeout, identity, or device-slot validation.
HA NOVA checkpoints the exact already-revoked device IDs before local deletion,
re-reads all OAuth slots under the mutation lock, deletes only the unchanged
snapshot, and preserves the checkpoint on any failure. Guided uninstall does
not expose a global override; it stops with the per-profile recovery command.
Grant deduplication includes canonical origin, client ID, and refresh token;
identical token text at two Home Assistant origins is two revocation targets.
Multi-profile purge plans every profile first, completes every remote device
and OAuth revocation next, and deletes native OAuth proof only after all remote
revocations succeed. Before the first local OAuth deletion, every profile gets
a durable checkpoint containing the exact slot metadata and a SHA-256 digest
of each high-entropy refresh token, never the token. A retry tolerates an
already deleted checkpointed slot, rejects any replacement slot, skips the
completed remote phase, and continues local deletion. Every destructive slot
step bypasses memoized reads, loads the fresh full native envelope under the
mutation lock, and deletes only an exact envelope match; a same-generation
replacement therefore fails closed. The Owner-confirmed
manual path records the same checkpoint plus the exact prior attestation, even
when no readable OAuth slot remains. Status and unlock direct this state only
to verified cleanup, never health verification or reconnect.

A malformed install-wide `client_install_id` never authorizes setup or device
use. Status still reports the selected Cloud lifecycle with the stable
`cloud_config_invalid` security-stop result and exact cleanup command. Verified
Cloud removal may preserve that exact malformed non-secret value while
revoking access; it cannot replace or delete the immutable value. After every
profile is proven free of Cloud lifecycle metadata, `cloud status` directs the
user to `ha-nova setup`. That explicit command may replace only the malformed
non-secret identity under the global mutation lock, an exact setup/config
snapshot, supported-schema validation, and unique profile-ID validation. It
preserves every profile and unknown field; normal loading then resumes. For a
named profile, the advertised repair-only setup command returns success
immediately after that verified identity repair instead of falling through to
the local-pairing guard.
Unscoped top-level Cloud lifecycle data is not attributed to the selected
profile and therefore yields a manual-review security stop without a
self-referential recovery command. A failed second config-document read is
also a manual security stop; it never suppresses recovery merely because the
new document could not be inspected.

`ha-nova server rename` is a metadata-only operation. It rejects any profile
with a non-null Cloud lifecycle and requires both source and destination
current/pending device namespaces, including raw file slots, to be readable and
empty. Paired profiles must be removed and re-added under the new name. This
prevents a valid bearer from being stranded under the old name, duplicated
under the new name, or hidden behind an unreachable keyring.

`ha-nova server remove` durably checkpoints the exact profile identity,
credential service names, observed presence or absence, SHA-256 evidence for
the complete raw current and pending credentials, and original profile
generation before remote revocation or native deletion. The digest never
stores the bearer and distinguishes replacement secrets that retain the same
remote device ID. Current and pending endpoint metadata must each be either
complete or absent before the checkpoint. The profile remains in `config.json`
until both routed slots and both raw file slots are proven absent. A crash
after the checkpoint, either revoke attempt, either slot deletion, or
immediately before profile removal resumes from the same command without
another confirmation. Observed slot evidence is inventory only; a separate
durable processed outcome records whether each exact slot was revoked, failed,
or was not applicable. Full uninstall purge writes the same processed outcome
before deleting either slot and performs the same final routed-and-raw
namespace proof after its global file-residue sweep. That proof rescans the
entire raw credential directory, marker, and every configured routed
namespace, so an orphan profile omitted from config cannot reappear unnoticed.
Malformed current or pending values remain removable by full purge with an
explicit unrevoked-cleanup report; native-store access failures still block.
A second global and per-profile proof runs after all later uninstall work and
immediately before config inventory deletion. Any reappearance preserves both
the replacement credential and `config.json` for retry. A slot absent at the
checkpoint but appearing later, or a slot present at the checkpoint but
disappearing before its processed outcome is durable, blocks profile deletion
for manual review. A final fresh routed-and-raw namespace proof runs
immediately before the profile document is removed. No live bearer namespace
becomes uninventoried.

The interactive wizard offers:

- `Local only` — recommended default and unchanged existing behavior;
- `Local + Home Assistant Cloud` — explicit opt-in to remote access;
- `Home Assistant Cloud only` — remote-first setup with pairing v2.

Initial and resumed Cloud URL entry use the same strict HTTPS-origin prompt.
Each entered alias is resolved to its canonical Nabu Casa origin inside that
prompt loop. Invalid syntax and an origin that does not verify as Nabu Casa stay
in the prompt until the user supplies a valid URL or explicitly exits. DNS
transport failures and security-sensitive resolver outcomes stop fail-closed.
The verified origin is returned directly to setup so it is not resolved again
between validation and use. Cancellation reports any already-saved profile
checkpoint and its exact profile-scoped recovery command instead of implying
that no state exists.

Wizard navigation is reversible until a remote mutation starts. `back` from
connection mode returns to client selection; `back` from Cloud URL entry returns
to connection mode. A saved hybrid checkpoint is always surfaced for default
and named profiles, interactive and non-interactive setup, and enabled or
disabled builds. Disabled builds offer verified cleanup only; enabled builds
also show the exact add/reconnect resume command.

Recovery guidance resolves the selected profile without falling back to the
literal default profile. An invalid or missing configured default, malformed
explicit/environment selection, reserved existing name, or unknown environment-only name suppresses every mutating recovery command until repaired.
Only a valid explicit `--server` remains creation intent. Non-interactive Cloud-only
setup under a recovery hold exposes only the exact profile-scoped
verified-remove command; unlock and setup resume remain interactive operations.
A machine-readable status for a secure-storage verification hold includes the
exact profile-scoped `cloud unlock` command; security-stop holds remain
commandless.

The MVP does not probe Home Assistant Cloud in the background before this
choice. That keeps the default private, avoids an extra network/authentication
step, and makes the visible recommendation match the actual wizard default.
An existing paired local install reuses its credential after matching the Relay
instance and does not request another code. Service/headless installs explain
that Cloud is desktop-only and remain local. A standard Home Assistant user is
recommended; owner/admin is allowed but not required for normal machine calls.
When remote pairing is required, OAuth completes first; a standard user is
preferred, while an Owner OAuth account remains supported. The wizard then
requires a separate private window or browser profile signed in as an Owner to
open NOVA and generate the one-time device code. It never auto-opens the Owner
capability URL in the OAuth session. A malformed, expired, inactive, or
rejected code stays in the same guided step and requests a fresh code; network,
rate-limit, protocol, and ambiguous failures never retry automatically. Human
input has no shared outer deadline. While the wizard waits for the Owner, it
releases the global client-mutation lock, then reacquires it and proves that
config.json is unchanged before consuming the code.
The same pause/reacquire rule applies to the longer OAuth browser wait, with
the additional guarantee that a returned code is never exchanged after config
drift. After reacquiring the mutation lock and before consuming the one-time
code, setup performs a new writable OAuth-keyring write/read/delete proof. At
most the first proof operation may show native UI; the read, cleanup, token
exchange follow-up, and pending-token write remain no-prompt. A keyring that
relocked while the browser was open therefore fails before code exchange.

An interactive macOS or Linux setup may request native keyring UI only after
the full local desktop, non-root, non-SSH, non-WSL guard passes. Operational
credential reads, writes, deletes, health checks, and cleanup remain no-prompt
and use `ForbidUI`.

## Supported beta contexts

- macOS desktop terminal
- Windows console and RDP after real-device validation
- validated Linux desktop Secret Service providers

WSL, SSH, services, gateways, and containers remain local-only until a separate
credential-broker design is reviewed and validated.

## Release availability

`version.json` is the release source of truth:

```json
{
  "cloud_remote_enabled": false,
  "cloud_remote_platforms": []
}
```

The root and App copies must match. Ordinary `go build` output is compile-time
disabled. Public builds fail closed unless they use the
`cloudremote_official` build tag, Cloud is enabled, the OS is listed, linker
and bundle exactly match `X.Y.Z[-rcN]`, the root skill version matches their
`X.Y.Z` base, the executable is the installed file, and Ed25519 bundle evidence
binds its SHA-256, platform, version, enabled platform list, and reviewed Git
tree. The public key remains deliberately unprovisioned until the separate
activation review.
The Relay keeps its owner Ingress page and local pairing path when disabled, but
registers only `/cloud/v1/info` plus exact device self-revocation for cleanup.
The info response advertises cleanup-only capabilities. Cloud setup, pairing,
and functional machine routes remain unregistered.

An explicitly stamped developer binary may enable one linker-injected isolated
test App slug. No environment/config override exists; release binaries ignore
the slug, keeping real-device testing separate from the production App.

Disabling a previously tested build must not strand credentials. `cloud unlock`,
`cloud remove`, uninstall purge, server removal, and `server route local` remain
available for cleanup. New authorization, reconnect, Cloud route selection, and
Cloud setup/resume remain blocked.

## User flows

- Existing local install: reuse and bind its credential after matching the
  Relay instance; no URL or second code is required.
- New install at home: complete local pairing once, then add Cloud.
- Remote-first: authorize the canonical origin, prepare the App, then pair via
  Ingress using an Owner-generated code. Open only the `/app/<slug>` frontend;
  never open or print the private machine capability.

## Release gates

- Full route parity and bounded Ingress memory under a 10,000-command stress run.
- Real keyrings on every advertised OS and all Home Assistant user roles.
- Default/custom domains, MFA, recovery/concurrency, App lifecycle, inactive
  subscriptions, and disabled remote access.
- Redirect rejection and credential non-disclosure tests at every network hop.
- Crash-point tests for every durable-state transition and credential rotation.
- Proof that local automatic fallback occurs only before functional dispatch.

The feature stays unavailable if any security or full-parity gate fails. The
GitHub `production` environment accepts only branch `main` and tags matching
`v*`; a read-only verifier blocks source, RC, and release gates on policy
drift. A trusted default-branch `workflow_run` broker inspects the pull request
merge source only as data and emits the required check through a dedicated,
repository-scoped GitHub App with Administration read and Checks write, bound
as the expected status source. Every success requires strict up-to-date branch
protection and the exact App binding. The broker binds the first fetched PR or
merge-queue ref to the verifier, re-fetches that ref immediately before
success, and then resolves the current PR identity and merge commit once more.
Requested and in-progress CI lifecycle events idempotently create one pending
App check per upstream run ID, attempt, and exact synthetic merge target; only
completion verifies and finishes it. Terminal success is idempotent, terminal
failure is retryable, and conflicting duplicate conclusions fail closed. A
read-only trigger job authenticates the exact check name, App ID, and slug
before its completion may re-evaluate Dependabot.

Dependabot never leaves native queued auto-merge enabled. A trusted direct
squash merger twice revalidates the current open Dependabot PR, exact head and
up-to-date base, current merge ref and `merge_commit_sha`, automation-owned
policy marker and label, exact live branch protection, every required check,
the latest dedicated-App source check's exact merge target, and absence of a
queued or running CI workflow. The final GitHub merge request supplies the
expected head SHA. Policy drift and explicit removal of the automation-owned
safe label disable any previously queued native auto-merge and remove only
automation-owned state; human or unauthenticated state stays untouched.
Safe-lane preparation dispatches one trusted current-default-branch
re-evaluation after writing the exact policy marker, covering checks that
finished before `ready_for_review`; incomplete checks remain a non-error no-op.
Bot-owned legacy native auto-merge is disabled before pagination-dependent
marker recovery, while human-owned native auto-merge is never changed.

GitHub Actions `workflow_run` delivery cannot synchronously invalidate a prior
same-target result. Production activation therefore remains fail-closed behind
the future required `cloud-source-invalidator` check from the dedicated
`markusleben-ha-nova-cloud-source-invalidator` App. Its policy App ID remains
`0` until an external service is provisioned and independently verified to
publish pending evidence before merge eligibility can be reused, bind success
to the current synthetic merge target, and invalidate that evidence on every
PR/base/head or CI-generation transition. Before activation, a separate
reviewed provisioning rollout must place both positive App IDs in policy,
require both checks, bind both exact Apps in live strict protection, and pass
the live verifier. Disabled production keeps both unprovisioned checks outside
the routine required list, but direct Dependabot merging remains intentionally
paused until live main protection is separately changed from `strict=false` to
the reviewed policy's `strict=true`. The merger never uses native queued
auto-merge and never performs a direct REST merge while live protection is
non-strict;
the invalidator check uses the exact external-ID grammar
`pull-request:<number>:target:<40-lowercase-hex-merge-sha>`, and the direct
merger rejects any other target. Actions-only evidence is insufficient. An
enabled target may change an existing non-sensitive workflow only by advancing
a full action commit SHA within the same canonical minor/patch release line on
an unchanged `uses:` identity;
workflow additions, deletions, renames, mode changes, action-identity changes,
other YAML changes, and every sensitive-workflow change fail closed.

CI and release workflows reject enabled metadata without structured evidence
that exactly identifies its own commit and full Git tree. Evidence may cover a
newer target only when its commit is an ancestor and the complete tree delta
contains exclusively those permitted existing non-sensitive `uses:` version
changes. Every product, metadata, script, or sensitive-workflow delta requires
fresh evidence for the exact target. Signed install-bundle provenance always
binds the current release tree, and exact uploaded bundles are smoke-tested on
every supported runner before a draft can be published. Disabled metadata
needs an empty platform list and no Cloud evidence. Enabled RC and final
publication additionally mint a short-lived token scoped only to
`Administration: read` from the exact policy-bound source App and revalidate
live strict, exact-App main protection before reading evidence.
