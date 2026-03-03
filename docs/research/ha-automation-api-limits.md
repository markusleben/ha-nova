# HA Automation API Limits: What a Mac-Side Setup Wizard Can Do

Date: 2026-03-02
Status: Research complete

## Executive Summary

A setup wizard running on a user's Mac faces **hard limits** imposed by HA's security architecture. The Supervisor API (addon install, repo management) is **not accessible externally** -- it is locked to the internal Docker network. LLAT creation is **not possible via REST API** -- only via WebSocket after full OAuth2 auth. The practical path is a **hybrid approach**: automate what's possible via API, guide the user through browser-based steps for the rest.

---

## 1. Add-on/App Installation via API

### Supervisor API Endpoints (Internal Only)

The Supervisor exposes these endpoints for addon management:

| Endpoint | Method | Purpose |
|---|---|---|
| `/store/repositories` | POST | Add a custom addon repository |
| `/store/repositories` | GET | List configured repositories |
| `/store` | GET | List available addons |
| `/store/addons/<addon>/install` | POST | Install an addon (preferred) |
| `/addons/<addon>/install` | POST | Install an addon (deprecated) |
| `/addons/<addon>/info` | GET | Get addon details |

All require `Authorization: Bearer SUPERVISOR_TOKEN` header.

**Source**: [Supervisor API Endpoints](https://developers.home-assistant.io/docs/api/supervisor/endpoints/)

### The Hard Wall: No External Access

**The Supervisor API is NOT accessible from outside the Docker network.** This is confirmed at source-code level.

In `homeassistant/components/hassio/http.py` (HA Core), the `HassIOView` class proxies `/api/hassio/{path}` to the Supervisor but applies strict path filtering:

```python
# What PATHS_ADMIN allows (the ONLY paths accessible via LLAT + admin):
PATHS_ADMIN = re.compile(
    r"^(?:"
    r"|backups/[a-f0-9]{8}(/info|/download|/restore/full|/restore/partial)?"
    r"|backups/new/upload"
    r"|.../logs..."
    r"|addons/[^/]+/(changelog|documentation)"
    r"|addons/[^/]+/logs..."
    r")$"
)
```

**Critically absent from PATHS_ADMIN**:
- `store/repositories` -- cannot add repos
- `store/addons/*/install` -- cannot install addons
- `addons/*/install` -- cannot install addons
- `addons/*/start` -- cannot start addons
- `addons/*/options` -- cannot configure addons

**The proxy only exposes**: backups, logs, addon changelog/documentation. That's it.

Even with a valid LLAT and admin user, you **cannot** install addons or add repositories via `/api/hassio/` from outside.

**Source**: [HA Core hassio/http.py](https://github.com/home-assistant/core/blob/dev/homeassistant/components/hassio/http.py), [Community confirmation](https://community.home-assistant.io/t/supervisor-external-api-access/428649)

### Chicken-and-Egg Problem

There is a genuine chicken-and-egg problem:
- To install the Relay addon, you need Supervisor API access
- Supervisor API is only available from inside the Docker network
- To get inside the Docker network, you need... an addon running

**Conclusion**: Addon installation **must** be a manual user step (or guided via browser).

---

## 2. LLAT Creation via API

### REST API: NOT Possible

There is **no REST API endpoint** for creating Long-Lived Access Tokens. The HA REST API (`/api/*`) requires a Bearer token for all endpoints. There is no public/unauthenticated endpoint at all:

> "All API calls have to be accompanied by the header `Authorization: Bearer TOKEN`."

**Source**: [HA REST API docs](https://developers.home-assistant.io/docs/api/rest/)

### WebSocket API: Possible (with prerequisite auth)

LLATs can be created via WebSocket command:

```json
{
    "id": 11,
    "type": "auth/long_lived_access_token",
    "client_name": "ha-nova-relay",
    "client_icon": null,
    "lifespan": 365
}
```

Response:
```json
{
    "id": 11,
    "type": "result",
    "success": true,
    "result": "eyJ0eXAiOiJKV1QiLCJhbGci..."
}
```

The token string is **not saved** in HA -- must be stored immediately. Valid for up to 10 years (lifespan in days).

**But**: You must first have an authenticated WebSocket connection, which requires an access token.

**Source**: [Authentication API](https://developers.home-assistant.io/docs/auth_api/)

### Full OAuth2 Flow for CLI Tool

A CLI tool can obtain an access token via HA's OAuth2 flow (IndieAuth-style):

**Step 1** -- Open browser to authorize:
```
http://<HA_HOST>:8123/auth/authorize?
  client_id=http%3A%2F%2F127.0.0.1%3A8400&
  redirect_uri=http%3A%2F%2F127.0.0.1%3A8400%2Fcallback&
  state=<random>
```

**Step 2** -- User logs in via HA frontend, grants access.

**Step 3** -- HA redirects to `http://127.0.0.1:8400/callback?code=<code>&state=<state>`.

**Step 4** -- Exchange code for token:
```bash
curl -X POST http://<HA_HOST>:8123/auth/token \
  -d "grant_type=authorization_code&code=<code>&client_id=http://127.0.0.1:8400"
```

Response: `{ "access_token": "...", "expires_in": 1800, "refresh_token": "...", "token_type": "Bearer" }`

**Step 5** -- Connect WebSocket with the access token:
```
ws://<HA_HOST>:8123/api/websocket
→ { "type": "auth", "access_token": "<access_token>" }
← { "type": "auth_ok", "ha_version": "2025.x" }
```

**Step 6** -- Create LLAT via WebSocket:
```json
{ "id": 1, "type": "auth/long_lived_access_token", "client_name": "ha-nova-relay", "lifespan": 3650 }
```

**Constraints**:
- `client_id` must be a URL; `redirect_uri` must share the same host+port
- CLI must spin up a temporary HTTP server on localhost to receive the callback
- Access tokens expire in 30 minutes; refresh tokens persist until revoked
- The user must be logged into HA and grant consent in the browser

### Chicken-and-Egg for LLAT

**Problem**: The Relay addon needs an LLAT in its config. But to create an LLAT programmatically, you need to go through the full OAuth2 flow. A simpler alternative is to guide the user to their profile page where they can manually create one.

**Verdict**: Programmatic LLAT creation via OAuth2+WebSocket is **technically possible** but complex. The ROI compared to "open this URL and paste the token" is questionable for an MVP.

---

## 3. What Other HA Automation Tools Do

### HACS Installation Pattern

HACS uses a **two-track bootstrap**:

**HA OS/Supervised:**
1. User adds `https://github.com/hacs/addons` as a repository (via UI)
2. Installs "Get HACS" addon from the addon store (via UI)
3. Starts the addon (via UI)
4. Follows log instructions for GitHub OAuth device auth

**Docker/Core:**
1. User runs: `wget -O - https://get.hacs.xyz | bash -`
2. Restarts HA
3. Adds HACS integration via UI

**Key insight**: HACS does NOT attempt to automate the addon installation. It relies entirely on manual UI steps for HA OS users.

**Source**: [HACS download docs](https://www.hacs.xyz/docs/use/download/download/), [HACS 2.0 blog post](https://www.home-assistant.io/blog/2024/08/21/hacs-the-best-way-to-share-community-made-projects/)

### Standard Pattern for "Install My Addon"

The industry standard for HA addon onboarding is:

1. Provide a `my.home-assistant.io` button in README/docs
2. User clicks button, gets redirected to their HA instance
3. User confirms repo addition in HA UI
4. User installs addon from store page
5. User configures addon options manually

No addon repo has successfully automated steps 3-5 from outside.

---

## 4. CLI Wizard Best Practices

### Discovery: No Public Endpoint

**All HA REST API endpoints require authentication**. There is no unauthenticated discovery endpoint.

`GET /api/` returns `{"message": "API running."}` but requires auth.

**Practical workaround**: Check if the host responds on port 8123 (TCP connect), or try the frontend HTML page (which loads without auth).

### Deep Links via my.home-assistant.io

The `my.home-assistant.io` service provides browser redirects to specific HA pages. Relevant redirects:

| Redirect | URL | Parameters |
|---|---|---|
| Add addon repository | `https://my.home-assistant.io/redirect/supervisor_add_addon_repository/` | `repository_url` (URL) |
| Addon dashboard | `https://my.home-assistant.io/redirect/supervisor_addon/` | `addon` (string), `repository_url` (URL, optional) |
| Addon store | `https://my.home-assistant.io/redirect/supervisor_store/` | none |
| User profile | `https://my.home-assistant.io/redirect/profile/` | none |

**Example**: Add ha-nova repo:
```
https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https://github.com/markusleben/ha-nova-addon
```

**Example**: Open Relay addon page:
```
https://my.home-assistant.io/redirect/supervisor_addon/?addon=local_ha_nova_relay
```

**Example**: Open profile page (for LLAT creation):
```
https://my.home-assistant.io/redirect/profile/
```

**Requirement**: User must have previously configured their HA instance URL in `my.home-assistant.io` settings (first-time redirect prompts for this).

**Source**: [my.home-assistant.io redirect.json](https://github.com/home-assistant/my.home-assistant.io/blob/main/redirect.json), [My Home Assistant docs](https://www.home-assistant.io/integrations/my/)

### Direct Frontend URLs (when HA host is known)

If the wizard knows the HA host, direct URLs work without the `my.home-assistant.io` intermediary:

```
# Addon store
http://<HA_HOST>:8123/hassio/store

# Specific addon page
http://<HA_HOST>:8123/hassio/addon/<addon_slug>/info

# User profile (for LLAT)
http://<HA_HOST>:8123/profile

# Supervisor settings (add repo)
http://<HA_HOST>:8123/hassio/store
```

The user must be logged in; these URLs will redirect to login if not.

### HA Companion App Deep Links

For mobile/native linking:
```
homeassistant://navigate/hassio/addon/<addon_slug>/info
homeassistant://navigate/profile
```

**Source**: [URL Handler docs](https://companion.home-assistant.io/docs/integrations/url-handler/)

---

## 5. Recommended Setup Wizard Strategy

Given the constraints above, here is the feasible automation boundary:

### What the CLI CAN Do
- [x] TCP probe HA host on port 8123 (reachability check)
- [x] Store config in `~/.config/ha-nova/onboarding.env`
- [x] Store LLAT in macOS Keychain
- [x] Open browser to specific HA pages (deep links)
- [x] Validate LLAT by calling `GET /api/` with Bearer token
- [x] Probe Relay health via `GET /health` on relay port
- [x] Full OAuth2 flow to programmatically create LLAT (complex but possible)

### What the CLI CANNOT Do
- [ ] Install the Relay addon (Supervisor API not exposed)
- [ ] Add a custom addon repository (Supervisor API not exposed)
- [ ] Configure addon options (Supervisor API not exposed)
- [ ] Start/stop addons (Supervisor API not exposed)
- [ ] Create LLAT without user browser interaction
- [ ] Detect HA version without auth

### Recommended Hybrid Flow

```
CLI Wizard                           User (Browser)
──────────                           ──────────────
1. Ask for HA host/URL
2. TCP-probe :8123 ──────────────→   (validates reachable)
3. Open browser: add-repo link ───→  4. User confirms repo in HA UI
5. Open browser: addon page ──────→  6. User installs + configures addon
7. Open browser: profile page ────→  8. User creates LLAT, copies it
9. Ask user to paste LLAT
10. Store LLAT in Keychain
11. Validate LLAT via GET /api/
12. Probe Relay /health
13. Done ✓
```

### Alternative: OAuth2 Automated LLAT (Advanced)

```
CLI Wizard                           User (Browser)
──────────                           ──────────────
1. Ask for HA host/URL
2. Start localhost:8400 HTTP server
3. Open browser: /auth/authorize ──→ 4. User logs in + grants access
5. Receive callback with auth code
6. Exchange code for access token
7. WebSocket: create LLAT
8. Store LLAT in Keychain
9. (still need manual addon install steps)
```

This eliminates the "paste the token" step but adds implementation complexity (HTTP server, WebSocket client, OAuth2 flow). May be worth it for UX polish post-MVP.

---

## Sources

- [Supervisor API Endpoints](https://developers.home-assistant.io/docs/api/supervisor/endpoints/)
- [HA Core hassio/http.py (proxy source)](https://github.com/home-assistant/core/blob/dev/homeassistant/components/hassio/http.py)
- [Supervisor External API Access (community thread)](https://community.home-assistant.io/t/supervisor-external-api-access/428649)
- [HA Authentication API](https://developers.home-assistant.io/docs/auth_api/)
- [LLAT via WebSocket (community thread)](https://community.home-assistant.io/t/how-to-create-a-long-lived-token-programmatically-using-web-socket-username-and-password/593468)
- [LLAT via REST API (community thread)](https://community.home-assistant.io/t/long-lived-access-token-using-websocket-api-or-rest-api/404755)
- [HA REST API docs](https://developers.home-assistant.io/docs/api/rest/)
- [Issue #89919: LLATs vs hassio endpoints](https://github.com/home-assistant/core/issues/89919)
- [HACS download docs](https://www.hacs.xyz/docs/use/download/download/)
- [HACS 2.0 announcement](https://www.home-assistant.io/blog/2024/08/21/hacs-the-best-way-to-share-community-made-projects/)
- [my.home-assistant.io redirect.json](https://github.com/home-assistant/my.home-assistant.io/blob/main/redirect.json)
- [My Home Assistant integration docs](https://www.home-assistant.io/integrations/my/)
- [URL Handler (Companion)](https://companion.home-assistant.io/docs/integrations/url-handler/)
- [Create addon repository docs](https://developers.home-assistant.io/docs/add-ons/repository/)
- [Supervisor External Access v2 (community thread)](https://community.home-assistant.io/t/supervisor-external-access-yes-i-know/763528)
