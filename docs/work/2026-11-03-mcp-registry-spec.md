# MCP Registry listing

Status: active

Date: 2026-11-03. Owner: release metadata (`server.json`, `scripts/bump-version.sh`, `scripts/release/verify-release-metadata.sh`, `.github/workflows/release.yml`). Issue: MAR-24.

## Goal

HA NOVA shows up in the official MCP Registry (registry.modelcontextprotocol.io), so MCP clients and directories that read the registry can find it. Each final release updates the entry by itself; nobody publishes by hand.

## Entry

One server, `io.github.markusleben/ha-nova`, with one remote:

- `streamable-http` at `https://{home_assistant_address}/api/ha_nova/mcp`. The address is a required URL variable: the Home Assistant Link address (`<id>.ui.nabu.casa`) or the home's own public HTTPS host. This is the chat-app endpoint the integration serves; the client signs in with the person's own Home Assistant login.

Why this entry and no package:

- The endpoint is what a registry-driven client can install without a terminal: paste the address, sign in. That matches the audience (UX first).
- `ha-nova mcp` (stdio) needs `ha-nova setup` and a pairing first, so a registry install alone would not work. Listing it needs an MCPB bundle per platform as a new signed release asset: new release machinery for an entry that still needs the installer. Deferred; revisit when setup can run from the bundle.
- An OCI entry does not fit: the App image is a Home Assistant App, not an MCP server a client can start.
- A remote-only entry has no package ownership check; the namespace is proved by GitHub OIDC from this repository (`io.github.markusleben/*`). The registry does not check that the URL belongs to the publisher, and a templated host is allowed (only `localhost` and non-HTTPS are refused).

## Version

`server.json` `version` equals `version.json` `skill_version` (SSOT). `scripts/bump-version.sh` writes it with the other manifests; `scripts/release/verify-release-metadata.sh` fails a tag whose `server.json` version, name, or remote differs. A published registry version is immutable, so the workflow skips the publish when the registry already lists that version (a retried run stays green).

## Publish

Job `publish-mcp-registry` in `release.yml`:

- runs after `smoke-installers` (so after `smoke-release-bundles` and `publish-release`), only for a final tag (`vX.Y.Z`, no `-rcN`);
- permissions `id-token: write`, `contents: read`; no secret;
- downloads `mcp-publisher` at a pinned release and checks its SHA-256 before running it;
- `mcp-publisher login github-oidc`, then `mcp-publisher publish`.

A failed publish leaves the GitHub release as it is; the registry entry is updated on the next final tag or a re-run of the job.

## Not in scope

- Manual directory listings (submitted by the maintainer with the text from the PR report).
- MCPB bundle for the stdio server (see above).

## Known limits

- Registry is in preview; a data reset there drops the entry until the next final tag.
- The registry binds GitHub OIDC to the repository owner, so any workflow of `markusleben` with `id-token: write` could publish this namespace; this job is the only one that does.
- A version deleted in the registry reads as unpublished, and the publish then fails loud with "already exists".
- The icon URL points at `main`; a later icon change shows on older registry versions too.
- Clients that do not support URL variables show the template; the README chat-app steps stay the main path.
