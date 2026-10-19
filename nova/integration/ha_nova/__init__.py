"""HA NOVA: passes chat-app requests on /api/ha_nova/mcp to the NOVA App.

A pure pass-through (docs/work/2026-10-19-chat-bridge-contract.md, B3):
Home Assistant authenticates the caller (its own sign-in, OAuth through
Home Assistant Link); the bridge in the NOVA App does everything else. This
file never reads, parses, or logs a body, and never logs the secret.
"""

from http import HTTPStatus
import logging

import aiohttp
from aiohttp import web

from homeassistant.components.http import KEY_HASS_USER, HomeAssistantView
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_create_clientsession
from homeassistant.helpers.typing import ConfigType

from .config_flow import DOMAIN

_LOGGER = logging.getLogger(__name__)

# Request headers passed on; everything else (Authorization, Cookie, any
# client-sent X-HA-User-Id) is dropped. Mcp-* headers pass by prefix.
_REQUEST_HEADERS = frozenset({"content-type", "accept", "last-event-id"})
_RESPONSE_HEADERS = frozenset({"content-type", "allow"})
# The bridge answers within its --confirm-wait (at most 2 min); more silence
# than this means it is stuck.
_TIMEOUT = aiohttp.ClientTimeout(total=None, sock_connect=10, sock_read=150)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """Register the view once; it answers only while an entry is loaded."""
    # Own session without a cookie jar: nothing from one user's exchange
    # (a Set-Cookie) may ride along on another user's request.
    session = async_create_clientsession(hass, cookie_jar=aiohttp.DummyCookieJar())
    hass.http.register_view(HaNovaView(hass, session))
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Nothing to start: the view reads the loaded entry on every request."""
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Nothing to stop: without a loaded entry the view answers 404."""
    return True


def _passes(name: str, allowed: frozenset[str]) -> bool:
    name = name.lower()
    return name in allowed or name.startswith("mcp-")


class HaNovaView(HomeAssistantView):
    """Streamable HTTP pass-through; the bridge validates method and headers."""

    url = "/api/ha_nova/mcp"
    name = "api:ha_nova:mcp"
    requires_auth = True

    def __init__(self, hass: HomeAssistant, session: aiohttp.ClientSession) -> None:
        """Keep hass for the entry lookup."""
        self.hass = hass
        self.session = session

    async def post(self, request: web.Request) -> web.StreamResponse:
        """Pass a JSON-RPC message on."""
        return await self._forward(request)

    async def get(self, request: web.Request) -> web.StreamResponse:
        """Pass on (the 0.27 bridge answers 405)."""
        return await self._forward(request)

    async def delete(self, request: web.Request) -> web.StreamResponse:
        """Pass on (the 0.27 bridge answers 405)."""
        return await self._forward(request)

    async def _forward(self, request: web.Request) -> web.StreamResponse:
        entries = self.hass.config_entries.async_loaded_entries(DOMAIN)
        if not entries:
            return self.json_message("Not found", HTTPStatus.NOT_FOUND)
        bridge = entries[0].data
        user = request.get(KEY_HASS_USER)
        # A signed path (authSig) is a link anyone holding it can open: no
        # chat-app sign-in. Users Home Assistant made for itself (Supervisor,
        # content) are nobody's chat app.
        if user is None or "authSig" in request.query:
            return self.json_message("Unauthorized", HTTPStatus.UNAUTHORIZED)
        if user.system_generated:
            return self.json_message(
                "This Home Assistant user can't use HA NOVA in chat apps.",
                HTTPStatus.FORBIDDEN,
            )

        headers = [
            (name, value)
            for name, value in request.headers.items()
            if _passes(name, _REQUEST_HEADERS)
        ]
        headers.append(("Authorization", f"Bearer {bridge['secret']}"))
        headers.append(("X-HA-User-Id", user.id))

        try:
            upstream = await self.session.request(
                request.method,
                f"http://{bridge['host']}:{bridge['port']}/mcp",
                headers=headers,
                data=request.content if request.body_exists else None,
                allow_redirects=False,
                timeout=_TIMEOUT,
            )
        except aiohttp.ConnectionTimeoutError:
            return self.json_message(
                "HA NOVA is not reachable. Check that the NOVA App is running.",
                HTTPStatus.BAD_GATEWAY,
            )
        except TimeoutError:
            return self.json_message(
                "HA NOVA took too long to answer. Try again.",
                HTTPStatus.GATEWAY_TIMEOUT,
            )
        except aiohttp.ClientError as err:
            _LOGGER.debug("HA NOVA bridge unreachable: %s", type(err).__name__)
            return self.json_message(
                "HA NOVA is not reachable. Check that the NOVA App is running.",
                HTTPStatus.BAD_GATEWAY,
            )

        async with upstream:
            if upstream.status == HTTPStatus.UNAUTHORIZED:
                # The bridge refused this entry's secret: never the caller's
                # Home Assistant sign-in, so no 401 that would make the chat
                # app sign in again and again.
                return self.json_message(
                    "HA NOVA's chat-app connection is out of date. "
                    "Turn chat apps off and on again in the NOVA App.",
                    HTTPStatus.BAD_GATEWAY,
                )
            response = web.StreamResponse(
                status=upstream.status,
                headers=[
                    (name, value)
                    for name, value in upstream.headers.items()
                    if _passes(name, _RESPONSE_HEADERS)
                ],
            )
            await response.prepare(request)
            try:
                async for chunk in upstream.content.iter_any():
                    await response.write(chunk)
                await response.write_eof()
            except (TimeoutError, aiohttp.ClientError, ConnectionResetError) as err:
                # The status is sent: cut the connection, so the client sees
                # a broken transfer, never a short answer that looks whole.
                _LOGGER.debug("HA NOVA stream ended early: %s", type(err).__name__)
                if request.transport is not None:
                    request.transport.abort()
            return response
