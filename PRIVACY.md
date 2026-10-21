# Privacy

Last updated: 2026-10-21

HA NOVA collects no usage data: no analytics, no installation counting, and
no reports to the maintainer. Update checks only fetch release information
from GitHub.

Earlier releases offered an opt-in Census that, only after an explicit yes,
sent a small installation report. The Census has been removed. The current
CLI never asks for it, never sends a Census report, and deletes its local
Census state on its first run. Updating sends no deletion request: an
installation that stops reporting has its record deleted automatically after
60 days, and all Census data is deleted when the Census service is shut down.
Older releases that were opted in keep reporting weekly until they are
updated.

## Chat apps (Claude and ChatGPT)

Chat apps are off by default. They need Home Assistant Link with remote access (a subscription from the Home Assistant team) or your own HTTPS address. When the owner turns them on, Claude or ChatGPT
connect straight to the owner's own Home Assistant, through Home Assistant
Link or the owner's own HTTPS address. HA NOVA runs no server of its own for this.
What you ask, and what HA NOVA reads from your home to answer, then goes to
Anthropic (Claude) or OpenAI (ChatGPT), and their privacy terms apply. Turning
chat apps off stops HA NOVA from answering them; to sign Claude or ChatGPT out of Home
Assistant, remove their refresh tokens in your Home Assistant profile.

## Demo house

The demo house (`ha-nova try`, Claude or ChatGPT via hanova.app/demo) is a shared
public Home Assistant that the maintainer runs. It is not your home. Other
visitors see what you enter. Don't enter personal data. What it
keeps, and for how long, is on its privacy page:
<https://hanova.app/privacy.html>.

## Optional Home Assistant Link transport

Home Assistant Link (formerly Home Assistant Cloud) remote access is available
only in release-gated desktop Beta builds. Selecting `Local only` sends no Home
Assistant traffic through Home Assistant Link. Selecting either Home Assistant
Link mode uses the user's Home Assistant Link service to reach that user's own Home
Assistant through Home Assistant OAuth and Supervisor Ingress. HA NOVA operates no
additional public tunnel, hosted broker, or Cloud endpoint for Home Assistant
requests.

The OAuth refresh token is stored only in a dedicated native OS credential
store. It is not written to HA NOVA config, a file fallback, an environment
variable, command arguments, logs, or AI-visible output. Short-lived OAuth
access tokens, Ingress paths, and Ingress cookies remain process-local and are
not persisted. The Relay still requires its per-device credential; through
Ingress that credential must be bound to both the authenticated Home Assistant
user and the persistent Relay instance.

Cloud setup, reconnect, and explicit unlock may request bounded native
secure-storage prompts for the selected credential slots. Normal Relay calls
use no-UI credential reads and fail closed when the store is locked or
unavailable.

`ha-nova cloud remove` revokes and verifies the HA NOVA OAuth authorization,
then removes its local Cloud secret and metadata. It does not cancel the Home
Assistant Link subscription or remove the local NOVA device pairing. Standard
uninstall keeps Home Assistant connection config, device pairing, and Cloud
authorization to support reinstall. `ha-nova uninstall --purge` first revokes
and verifies every configured Cloud authorization, then removes the related
local secrets and configuration; it stops before destructive local cleanup if
safe revocation cannot be completed.
