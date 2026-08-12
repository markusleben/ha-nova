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

Before creating a machine session, the CLI verifies the exact selected App
slug, started state, version, Ingress root, and `/home` UI entry. Supervisor
may report that UI entry as either `<root>/home` or `<root>//home` when the App
configuration contains the leading slash. Only those two exact forms are
accepted; generic path normalization is forbidden.

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
complete native envelope, and asks the native backend to recheck and delete the
same encoded value in one HA NOVA backend operation under the global
client-mutation lock. An observed-absent slot receives the same backend recheck
before its cache entry is invalidated. Ambiguous pending-grant cleanup also
binds the canonical Home Assistant origin beside generation, client ID, and
refresh token. A concurrently replaced, newly inserted, or cross-origin
envelope observed within this boundary is preserved as `SECRET_CONFLICT`;
successful or confirmed-absent deletion invalidates the access-session cache.

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
writers across processes. Native credential stores do not expose an atomic
compare-and-delete primitive. Direct modification by unrelated tools running
with the same user's credential-store authority is outside this integrity
boundary; HA NOVA makes no concurrency guarantee against such a process and
reports a conflict only when it observes the differing value.

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

Native-store E2E isolation must preserve the real desktop login `HOME`;
otherwise the OS may resolve no default keychain or secret collection.
`HA_NOVA_CONFIG_DIR` relocates only HA NOVA's non-secret config, checkpoints,
state, and census files so a unique test profile can use the real native store
without touching the production profile.

Cloud Beta stays blocked until macOS artifacts have a stable signing identity
and real update/reinstall tests prove selected-slot unlock plus no-UI reuse.
Broadening the Keychain ACL is forbidden. Hardened ad-hoc development signing
enables testing but is not release evidence.

RC and final macOS binaries are built on a GitHub-hosted macOS runner from the
exact workflow checkout, signed there with the publisher's Developer ID
Application certificate, and verified before leaving that job. The release
contract fixes the Team ID to `CTF9J94274`, the executable identifier to
`com.markusleben.ha-nova.cli`, and requires the `hard`, `kill`,
`library-validation`, and `runtime` Code Directory flags. The certificate and
its password exist only as protected `production` environment secrets; they are
removed from the environment before the Go build starts.

Linux and Windows continue to build on Ubuntu. GoReleaser does not build an
unsigned Darwin replacement. The bundle job accepts only the two signed Darwin
artifacts from the macOS job. macOS smoke verifies the signature again and
proves that the bundled executable is byte-identical to the corresponding raw
release asset before publication.

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
- `relay health --connect-timeout` bounds every network connection needed to
  select local, Cloud, or automatic transport, including OAuth refresh,
  WebSocket/Ingress discovery, and the automatic local preflight. The separate
  `--max-time` budget continues to bound the complete health command.
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
unreachable Cloud, incomplete setup, not-configured outcomes, and a failed
predispatch config-transaction recovery. Typed
`verification_error` and `next_command` fields let headless callers recover
without parsing human text. A clearable secure-storage hold first advances to
interactive `cloud unlock`. When health cannot clear the hold because the
profile is incomplete, cleanup is already checkpointed, or Cloud is disabled,
unlock durably records only the successful native-storage proof; the next
status advances to exact, profile-scoped `cloud remove`. A later storage-lock
failure from either the OAuth or device credential store resets that proof, so
recovery advances back to unlock instead of looping. A Cloud-capable upgrade
does not trust a proof recorded by a disabled build for a ready connection:
status offers unlock again so a successful Cloud health check can clear the
hold without destructive cleanup. A named Cloud-only profile may run
`ha-nova setup --server <name>` to resume Cloud onboarding and install client
skills. An explicit client-only target may also repair skills on an existing
named profile only through its already configured and freshly verified secure
device/Cloud transport. This dedicated path never reads the machine-wide
default-profile token and never enters connection mutation; named
local/token/service onboarding remains unavailable and uses `pair --server`.
Client-only dispatch occurs before retirement or pending-activation recovery
and finishes without invoking doctor, whose pairing recovery is intentionally
connection-mutating. If the install-wide client identity is malformed, named
client-only setup validates the selected profile and requested client first,
then stops without changing config. Its guidance resolves install-wide Cloud
cleanup across every profile before it can advertise identity repair and the
client retry. The separate profile-scoped setup repair must restore the install
identity before client installation is retried.

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
Immediately before full-purge config deletion, HA NOVA requires the exact
post-cleanup config snapshot to remain current, reloads the complete profile
inventory, and repeats configured-namespace plus global raw credential absence
proofs. A new or changed sibling/default/Cloud profile preserves config and
fails the purge retryably. The configured service-token file is removed while
config still durably identifies it, and the exact config bytes are checked
again inside the removal function after all credential proofs. Purge deletes
only the exact HA NOVA-managed service-token path. A configured alias,
overlapping config target, symlink-ancestor path, or any other nonstandard
target blocks deletion and preserves both that target and config for manual
review. If either native credential store relocks during a later purge proof,
every clearable verified storage hold is reset before returning so the next
recovery step is unlock rather than destructive cleanup.

A malformed install-wide `client_install_id` never authorizes setup or device
use. Status still reports the selected Cloud lifecycle with the stable
`cloud_config_invalid` security-stop result and exact recovery command. If a
valid recovery hold exists, `cloud unlock` may load only the safely selected
unchecked recovery snapshot through explicit, environment, or configured
default profile selection and prove both native stores without accepting the
malformed identity for normal use. A clearable hold advances its durable proof;
a non-clearable security hold remains unchanged while the successful prompt
permits immediate verified cleanup. Verified Cloud removal may then preserve
that exact malformed non-secret value while revoking access; it cannot replace
or delete the immutable value. All human and machine recovery renderers use the
same unlock-versus-remove decision. After every profile is proven free of Cloud
lifecycle metadata, `cloud status` directs the user to `ha-nova setup`. That
explicit command may replace only the malformed non-secret identity under the
global mutation lock, an exact setup/config snapshot, supported-schema
validation, and unique profile-ID validation. It preserves every profile and
unknown field; normal loading then resumes. For a named profile, the advertised
repair-only setup command returns success immediately after that verified
identity repair instead of falling through to the local-pairing guard.
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

## Release-prep README copy

The release-prep PR inserts this compact explanation after the Get Started
wizard description. It does not land in `README.md` before the version bump:

> ### Optional remote access with Home Assistant Cloud (Beta)
>
> The wizard keeps **Local only** as the recommended default. If you have a
> paid Home Assistant Cloud subscription from Nabu Casa with Remote UI enabled,
> choose **Local + Home Assistant Cloud** for automatic remote fallback or
> **Home Assistant Cloud only** for remote-first setup. The wizard validates
> your Cloud URL, opens Home Assistant OAuth, and stores the authorization in
> the native macOS, Windows, or Linux desktop credential store. HA NOVA runs no
> additional public tunnel or hosted broker.
>
> Cloud Remote requires Home Assistant OS/Supervised and a supported desktop
> session. Headless, SSH, WSL, service, gateway, Container, and Core setups stay
> local-only. Remote-first pairing uses a separate private Owner session to
> create the one-time NOVA device code; the OAuth user can remain a standard
> Home Assistant user.

## README visuals (0.22.0 release-prep)

The two release-prep README images below ship with the v0.22.0 release-prep
PR. The process follows the approved v0.19 asset pipeline
(`docs/archive/work/2026-07-20-asset-specs.md`): backgrounds are generated
star-free via `codex exec` (built-in `image_gen`, native 1774×887 output,
then a deterministic content-band crop + resize with `sips`), the canonical
star block is composited via wrapper SVGs in `docs/archive/work/`, and every PNG
renders at 2× supersampling (`rsvg-convert -w 2W -h 2H` → `sips -z H W`, run
from `docs/archive/work/`). Background rasters and wrapper SVGs are committed so a
fresh checkout reproduces both assets.

Shared rules (unchanged from the v0.19 system): byte-identical `star-grad-v3`
star block only — never a model-drawn star; backgrounds request "NO star, NO
sparkle"; the star never sits on AI pixels (dark inset behind it); no
glow/blur in the vector layer; cyan = client/local, amber = server/secure;
minimal exact text whitelist per image with a glyph check on every attempt and
up to 3 attempts, then the documented hybrid fallback (background without the
failing label + vector system-font label in the wrapper).

### Image A — Cloud fallback (`assets/cloud-fallback.png`, 1600×640)

Placement: inside "Optional remote access with Home Assistant Cloud (Beta)",
after the `ha-nova cloud add` paragraph. Scene: laptop wireframe left ("Your
machine"), Home Assistant house right ("Home Assistant") with a small empty
rounded app tile attached (the canonical star is composited there), one
straight bright cyan line with a padlock = preferred local path, one slightly
dimmer amber arc over a small cloud outline = "Home Assistant Cloud"
fallback. Only readable text: `Your machine`, `Home Assistant`,
`Home Assistant Cloud`.

Sources: `docs/archive/work/cloud-fallback-bg-nostar.png` +
`docs/archive/work/2026-07-31-cloud-fallback-composite.svg`.

Prompt (via `codex exec`, style-referenced with
`-i docs/archive/work/howitworks-bg-nostar.png`):

> Dark minimal network diagram, wide landscape composed for a wide center
> crop. Background: deep space navy gradient from #0A0E1A to #0A1628 with
> sparse tiny white stars at low opacity. Style exactly like the attached
> reference: thin luminous neon wireframe line-art, flat, straight-on, no 3D,
> no isometric perspective. LEFT: an open laptop in thin cyan #4FC3F7
> wireframe outline, label below in bold white sans-serif: "Your machine".
> RIGHT: a house silhouette in thin cyan wireframe with a warm amber door,
> label below: "Home Assistant"; attached to the upper edge of the house one
> small empty rounded-square app tile with a soft warm amber outline, its
> interior completely dark and empty. LOWER CENTER: one straight thin glowing
> cyan horizontal connection line from the laptop to the house with a small
> cyan padlock icon at its middle. UPPER CENTER: one thin glowing amber arc
> from the laptop rising over a small amber cloud outline at the top middle
> and descending to the house, with the label "Home Assistant Cloud" in bold
> white sans-serif near the cloud. The cyan line is the primary bright path;
> the amber arc is visible but slightly dimmer. Only readable text, exactly
> spelled, nothing else: "Your machine", "Home Assistant",
> "Home Assistant Cloud". NO star shapes, NO sparkles, no four-point stars
> anywhere. No watermarks, no extra text, no UI chrome.

### Image B — How it works v3 (`assets/how-it-works-v3.png`, 1600×700)

Replaces `assets/how-it-works-v2.png` (deleted in the same PR; historical
tags keep their copy). Same approved v2 composition plus exactly one amber
Cloud fallback arc ("Home Assistant Cloud") from the laptop over a small
cloud outline to the house. Only readable text: `Your machine`, `Skills`,
`NOVA Relay`, `428 316`, `Home Assistant`, `Home Assistant Cloud`.
Additional fallback for this image only: keep the approved v2 background
untouched and add arc + label as a crisp vector overlay in the wrapper.

Sources: `docs/archive/work/howitworks-bg-v3-nostar.png` +
`docs/archive/work/2026-07-31-how-it-works-v3-composite.svg`.

Prompt (via `codex exec`, edit-referenced with
`-i docs/archive/work/howitworks-bg-nostar.png`):

> Recreate the attached network diagram precisely: same layout, same objects,
> same colors, same label positions — an open laptop in thin blue wireframe,
> three markdown document sheets labeled "Skills", a straight thin cyan
> glowing line with a small cyan padlock leading to a central rounded-square
> app tile with a warm amber outline and a completely dark empty interior, an
> amber code chip reading "428 316" below it, the label "NOVA Relay" under
> the tile, then a short thin amber glowing line to a house wireframe labeled
> "Home Assistant". ADD exactly one new element: a thin glowing amber arc
> that rises from the laptop area, passes over a small amber cloud outline
> near the top center, and descends to the house on the right, with the label
> "Home Assistant Cloud" in bold white sans-serif near the cloud. Everything
> else stays unchanged: deep navy space background from #0A0E1A to #0A1628
> with sparse tiny dim white stars, flat straight-on neon wireframe style, no
> 3D. Only readable text, exactly spelled, nothing else: "Your machine",
> "Skills", "NOVA Relay", "428 316", "Home Assistant",
> "Home Assistant Cloud". The app tile interior stays completely dark and
> empty. NO star shapes, NO sparkles anywhere. No watermarks, no extra text.

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
  "cloud_remote_enabled": true,
  "cloud_remote_platforms": ["darwin", "linux", "windows"]
}
```

The root and App copies must match. Ordinary `go build` output is compile-time
disabled. Public builds fail closed unless they use the
`cloudremote_official` build tag, Cloud is enabled, the OS is listed, linker
and bundle exactly match `X.Y.Z[-rcN]`, the root skill version matches their
`X.Y.Z` base, the executable is the installed file, and Ed25519 bundle evidence
binds its SHA-256, platform, version, enabled platform list, and reviewed Git
tree. The compiled public key matches the protected production private key
through a committed non-secret verification signature; the private key never
enters source or artifacts.
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

- Full route parity on one real reference platform and bounded Ingress memory
  under one 10,000-command stress run after a Cloud or Relay transport change
  or stress-harness change. The hidden `internal-cloud-stress` command resolves
  one explicit Cloud transport, then performs exactly 10,000 read-only
  authenticated `/health` requests through that one process-local Ingress
  session. It has fixed per-request and overall deadlines, stops on the first
  redirect, transport, status, size, encoding, or Relay-identity failure, and
  never runs Census or update checks.
- Real keyring happy-path and fail-closed no-UI behavior on every advertised
  OS for first support. Shared orchestration changes repeat one reference OS;
  adapter changes repeat only the affected OS. Deterministic platform tests
  cover prompt cancellation and timeout.
- One real standard non-administrator role binding and one canonical Nabu Casa
  OAuth flow. Home Assistant owns its MFA challenge before returning the same
  OAuth callback. Deterministic authorization tests cover custom origins,
  inactive subscriptions, disabled remote access, and authorization abort.
- One isolated Cloud-authorized lifecycle profile first covers Relay App
  restart and reinstall recovery, then HA NOVA CLI standard uninstall/reinstall
  with retained authorization, then full purge last. The purge revokes and
  verifies the active remote authorization and device before local cleanup.
  Deterministic tests cover durable recovery and concurrency; update and
  instance mismatch receive real runs when those paths change.
- Redirect rejection and credential non-disclosure tests at every network hop.
- Crash-point tests for every durable-state transition and credential rotation.
- Proof that local automatic fallback occurs only before functional dispatch.

Real-device qualifications remain applicable across unrelated changes only
after reviewing the complete qualification-to-target diff and recording the
non-secret qualification ledger in the activation or release pull request.
Changes to a deterministic substitute or real evidence harness invalidate its
qualification.
Exact-target CI, candidate provenance on all enabled OSes, and the installed
Relay App never carry forward. One downloaded-candidate
`relay health --via cloud` smoke with Census suppressed repeats for deltas
that match an invalidation-map row with real-platform scope; maintenance
deltas refresh the envelope and provenance without it. The smoke must parse
the JSON and require the expected App version and `ha_ws_connected: true`; a
zero exit code alone is not evidence. See
`2026-07-30-cloud-release-evidence-risk-scope-spec.md`.

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
Each in-progress CI lifecycle event idempotently creates an attempt-bound
provisional pending App check without resolving or executing the target. A
successful completed event creates the exact synthetic-target check before
retiring that provisional check, closing the old-success rerun window. Failed
or cancelled CI retires its provisional check because CI remains failed.
Stale pull requests and temporarily absent merge refs emit no exact check and
remain fail-closed. Terminal success is idempotent, terminal failure is
retryable, and conflicting duplicate conclusions fail closed. A
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

The single dedicated source App is the complete MVP trust boundary. GitHub
requires a successful check on the latest candidate SHA, and strict branch
protection requires the branch to be current with its base. The trusted
default-branch broker first creates a provisional pending check for each active
CI attempt, then replaces it with the exact synthetic-target check before
reporting success. The
direct merger additionally rejects a source success whose external ID names
another merge target, any active current-head CI run, or any changed PR,
head, base, or merge ref. A second external invalidator service would duplicate
GitHub's latest-SHA and strict-update guarantees without adding a
repository-verifiable security boundary, so the MVP deliberately does not
require one.
Before activation, a reviewed provisioning rollout must place the positive
source App ID in policy, require its check, bind that exact App in live strict
protection, and pass the live verifier. Disabled production keeps the
unprovisioned check outside the routine required list, but direct Dependabot
merging remains intentionally paused until live main protection is separately
changed from `strict=false` to the reviewed policy's `strict=true`. The merger
never uses native queued auto-merge and never performs a direct REST merge
while live protection is non-strict. Actions-only evidence is insufficient. An
enabled target may change an existing non-sensitive workflow only by advancing
a full action commit SHA within the same canonical minor/patch release line on
an unchanged `uses:` identity;
workflow additions, deletions, renames, mode changes, action-identity changes,
other YAML changes, and every sensitive-workflow change fail closed.

CI and release workflows reject enabled metadata without structured evidence
that exactly identifies its own commit and full Git tree. Evidence may cover a
newer target only when its commit is an ancestor and the complete tree delta
contains exclusively those permitted existing non-sensitive `uses:` version
changes, or exclusively the guarded non-sensitive source delta (Markdown
under `docs/` or `skills/`, root Markdown other than `AGENTS.md`; see
`docs/releasing.md`). Every product, metadata, script, test, or
sensitive-workflow delta requires fresh evidence for the exact target. Signed install-bundle provenance always
binds the current release tree, and exact uploaded bundles are smoke-tested on
every supported runner before a draft can be published. Disabled metadata
needs an empty platform list and no Cloud evidence. Enabled RC and final
publication additionally mint a short-lived token scoped only to
`Administration: read` from the exact policy-bound source App and revalidate
live strict, exact-App main protection before reading evidence.
