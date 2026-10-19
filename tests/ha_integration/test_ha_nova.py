"""ha_nova: discovery-only setup and the /api/ha_nova/mcp pass-through."""

import asyncio
from datetime import timedelta
import logging
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch
import uuid as uuidlib

from aiohasupervisor import SupervisorError
from aiohasupervisor.models import Discovery
import aiohttp

from aiohttp import web
from aiohttp.test_utils import TestServer
import pytest

from homeassistant.components.hassio import discovery as hassio_discovery
from homeassistant.components.http.auth import async_sign_path
from homeassistant.config_entries import SOURCE_HASSIO, ConfigEntryState
from homeassistant.core import HomeAssistant
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers.service_info.hassio import HassioServiceInfo
from homeassistant.setup import async_setup_component

DOMAIN = "ha_nova"
URL = "/api/ha_nova/mcp"
SECRET = "ab" * 32
UUID = "0" * 31 + "1"
SLUG = "a0d7b954_ha_nova"


def info(port: int, *, uuid: str = UUID, slug: str = SLUG, secret: str = SECRET, host: str = "127.0.0.1"):
    return HassioServiceInfo(
        config={"host": host, "port": port, "secret": secret, "addon": "HA NOVA"},
        name="HA NOVA",
        slug=slug,
        uuid=uuid,
    )


async def discover(hass: HomeAssistant, discovery: HassioServiceInfo):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": SOURCE_HASSIO}, data=discovery
    )
    await hass.async_block_till_done()
    return result


class Bridge:
    """A stub `ha-nova serve`: records what arrives, answers what a test sets."""

    def __init__(self) -> None:
        self.seen: list[tuple[str, str, list[tuple[str, str]], bytes]] = []
        self.answer = self._json

    async def start(self) -> int:
        app = web.Application()
        app.router.add_route("*", "/{tail:.*}", self._handle)
        self.server = TestServer(app, host="127.0.0.1")
        await self.server.start_server()
        return self.server.port

    async def _handle(self, request: web.Request) -> web.StreamResponse:
        body = await request.read()
        self.seen.append((request.method, request.path_qs, list(request.headers.items()), body))
        return await self.answer(request)

    @staticmethod
    async def _json(request: web.Request) -> web.StreamResponse:
        if request.method != "POST":
            return web.Response(status=405, text="method not allowed", headers={"Allow": "POST"})
        return web.Response(
            body=b'{"jsonrpc":"2.0","id":1,"result":{}}',
            content_type="application/json",
            headers={"Mcp-Session-Id": "s-1", "X-Bridge-Internal": "no", "Set-Cookie": "a=b"},
        )


@pytest.fixture
async def bridge(hass: HomeAssistant):
    stub = Bridge()
    port = await stub.start()
    result = await discover(hass, info(port))
    assert result["type"] is FlowResultType.CREATE_ENTRY
    yield stub
    await stub.server.close()


async def test_discovery_creates_the_entry_without_a_user_step(hass: HomeAssistant) -> None:
    result = await discover(hass, info(8794))
    assert result["type"] is FlowResultType.CREATE_ENTRY
    [entry] = hass.config_entries.async_entries(DOMAIN)
    assert entry.state is ConfigEntryState.LOADED
    assert entry.source == SOURCE_HASSIO
    # Home Assistant removes a hassio entry whose unique ID is the uuid of the
    # discovery message the App deletes (components/hassio/discovery.py).
    assert entry.unique_id == UUID
    assert entry.data == {"host": "127.0.0.1", "port": 8794, "secret": SECRET, "slug": SLUG}


async def test_a_changed_message_updates_the_one_entry(hass: HomeAssistant) -> None:
    await discover(hass, info(8794))
    result = await discover(hass, info(8795, secret="cd" * 32))
    assert result["type"] is FlowResultType.ABORT
    [entry] = hass.config_entries.async_entries(DOMAIN)
    assert (entry.data["port"], entry.data["secret"]) == (8795, "cd" * 32)


async def test_a_new_message_from_the_same_app_replaces_the_old_entry(hass: HomeAssistant) -> None:
    await discover(hass, info(8794))
    new_uuid = "f" * 32
    result = await discover(hass, info(8794, uuid=new_uuid, secret="cd" * 32))
    assert result["type"] is FlowResultType.ABORT
    [entry] = hass.config_entries.async_entries(DOMAIN)
    assert (entry.unique_id, entry.data["secret"]) == (new_uuid, "cd" * 32)


async def test_another_app_cannot_take_over(hass: HomeAssistant) -> None:
    await discover(hass, info(8794))
    result = await discover(hass, info(9999, uuid="e" * 32, slug="other_ha_nova", host="evil"))
    assert result == result | {"type": FlowResultType.ABORT, "reason": "already_configured"}
    [entry] = hass.config_entries.async_entries(DOMAIN)
    assert (entry.unique_id, entry.data["host"]) == (UUID, "127.0.0.1")


@pytest.mark.parametrize(
    "config",
    [
        {"host": "x/evil", "port": 8794, "secret": SECRET},
        {"host": "nova", "port": "8794", "secret": SECRET},
        {"host": "nova", "port": 0, "secret": SECRET},
        {"host": "nova", "port": 8794, "secret": "short"},
        {"host": "nova", "port": 8794},
    ],
)
async def test_a_malformed_message_creates_nothing(hass: HomeAssistant, config: dict) -> None:
    result = await discover(hass, HassioServiceInfo(config=config, name="HA NOVA", slug=SLUG, uuid=UUID))
    assert result == result | {"type": FlowResultType.ABORT, "reason": "invalid_discovery_info"}
    assert hass.config_entries.async_entries(DOMAIN) == []


async def test_no_token_gets_home_assistants_401(hass: HomeAssistant, bridge: Bridge, hass_client_no_auth) -> None:
    client = await hass_client_no_auth()
    for method in ("POST", "GET", "DELETE"):
        response = await client.request(method, URL, data=b"{}", headers={"X-HA-User-Id": "f" * 32})
        assert response.status == 401
        assert "resource_metadata=" in response.headers.get("WWW-Authenticate", "")
    assert bridge.seen == []


async def test_post_passes_through_with_the_secret_and_the_authenticated_user(
    hass: HomeAssistant, bridge: Bridge, hass_client, hass_admin_user, caplog: pytest.LogCaptureFixture
) -> None:
    caplog.set_level(logging.DEBUG)
    client = await hass_client()
    body = b'{"jsonrpc":"2.0","id":1,"method":"tools/list"}' + b" " * 300_000
    response = await client.post(
        URL + "?q=1&token=x",
        data=body,
        headers={
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            "Last-Event-ID": "7",
            "Mcp-Protocol-Version": "2025-11-25",
            "Mcp-Session-Id": "s-1",
            "X-HA-User-Id": "f" * 32,
            "x-ha-user-id": "e" * 32,
            "Cookie": "session=1",
            "Proxy-Authorization": "Basic eDp5",
            "X-Other": "no",
        },
    )
    assert response.status == 200
    assert await response.read() == b'{"jsonrpc":"2.0","id":1,"result":{}}'
    assert response.headers["Content-Type"].startswith("application/json")
    assert response.headers["Mcp-Session-Id"] == "s-1"
    assert "X-Bridge-Internal" not in response.headers
    assert "Set-Cookie" not in response.headers

    [(method, path, headers, got)] = bridge.seen
    assert (method, path, got) == ("POST", "/mcp", body)
    lower = [(name.lower(), value) for name, value in headers]
    assert [v for n, v in lower if n == "authorization"] == [f"Bearer {SECRET}"]
    assert [v for n, v in lower if n == "x-ha-user-id"] == [hass_admin_user.id]
    sent = {n for n, _ in lower}
    assert {"content-type", "accept", "last-event-id", "mcp-protocol-version", "mcp-session-id"} <= sent
    assert sent.isdisjoint({"cookie", "proxy-authorization", "x-other"})
    assert SECRET not in caplog.text
    assert "tools/list" not in caplog.text


@pytest.mark.parametrize("method", ["GET", "DELETE"])
async def test_get_and_delete_get_the_bridges_405(hass: HomeAssistant, bridge: Bridge, hass_client, method: str) -> None:
    client = await hass_client()
    response = await client.request(method, URL)
    assert response.status == 405
    assert response.headers["Allow"] == "POST"
    assert [m for m, *_ in bridge.seen] == [method]


async def test_other_methods_never_reach_the_bridge(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    client = await hass_client()
    response = await client.put(URL, data=b"{}")
    assert response.status == 405
    assert bridge.seen == []


async def test_a_stream_passes_chunk_by_chunk(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    release = asyncio.Event()

    async def sse(request: web.Request) -> web.StreamResponse:
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await response.write(b"data: one\n\n")
        await release.wait()
        await response.write(b"data: two\n\n")
        await response.write_eof()
        return response

    bridge.answer = sse
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    assert response.headers["Content-Type"] == "text/event-stream"
    try:
        first = await asyncio.wait_for(response.content.readuntil(b"\n\n"), 5)
        assert first == b"data: one\n\n"
    finally:
        release.set()
    assert await response.content.read() == b"data: two\n\n"


async def test_status_passes_through(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    async def forbidden(request: web.Request) -> web.StreamResponse:
        return web.Response(status=403, text="This Home Assistant user may not use HA NOVA")

    bridge.answer = forbidden
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    assert (response.status, await response.text()) == (403, "This Home Assistant user may not use HA NOVA")


async def test_the_bridges_401_is_no_sign_in_prompt(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    async def refused(request: web.Request) -> web.StreamResponse:
        return web.Response(status=401, text="unauthorized")

    bridge.answer = refused
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    assert response.status == 502
    assert "Turn chat apps off and on" in (await response.json())["message"]


async def test_a_stopped_bridge_is_a_readable_502(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    await bridge.server.close()
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    assert response.status == 502
    assert "NOVA App is running" in (await response.json())["message"]


async def test_a_silent_bridge_is_a_readable_504(
    hass: HomeAssistant, bridge: Bridge, hass_client, monkeypatch: pytest.MonkeyPatch
) -> None:
    from custom_components import ha_nova  # noqa: PLC0415

    monkeypatch.setattr(ha_nova, "_TIMEOUT", aiohttp.ClientTimeout(total=None, sock_read=0.2))
    done = asyncio.Event()

    async def silent(request: web.Request) -> web.StreamResponse:
        await done.wait()
        return web.Response(text="late")

    bridge.answer = silent
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    done.set()
    assert response.status == 504
    assert "took too long" in (await response.json())["message"]


async def test_unload_and_remove(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    [entry] = hass.config_entries.async_entries(DOMAIN)
    assert await hass.config_entries.async_unload(entry.entry_id)
    assert entry.state is ConfigEntryState.NOT_LOADED
    client = await hass_client()
    assert (await client.post(URL, data=b"{}")).status == 404
    await hass.config_entries.async_remove(entry.entry_id)
    await hass.async_block_till_done()
    assert hass.config_entries.async_entries(DOMAIN) == []
    assert (await client.post(URL, data=b"{}")).status == 404
    assert bridge.seen == []


async def test_a_notification_gets_the_bridges_empty_202(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    async def accepted(request: web.Request) -> web.StreamResponse:
        return web.Response(status=202)

    bridge.answer = accepted
    client = await hass_client()
    response = await client.post(URL, data=b'{"jsonrpc":"2.0","method":"notifications/initialized"}')
    assert (response.status, await response.read()) == (202, b"")


async def test_a_cut_stream_is_a_broken_transfer(hass: HomeAssistant, bridge: Bridge, hass_client) -> None:
    async def dies(request: web.Request) -> web.StreamResponse:
        response = web.StreamResponse(headers={"Content-Type": "text/event-stream"})
        await response.prepare(request)
        await response.write(b"data: one\n\n")
        request.transport.abort()
        return response

    bridge.answer = dies
    client = await hass_client()
    response = await client.post(URL, data=b"{}")
    with pytest.raises(aiohttp.ClientPayloadError):
        await response.read()


async def test_home_assistants_own_users_are_refused(
    hass: HomeAssistant, bridge: Bridge, hass_client, hass_supervisor_access_token: str
) -> None:
    client = await hass_client(hass_supervisor_access_token)
    response = await client.post(URL, data=b"{}")
    assert response.status == 403
    assert bridge.seen == []


async def test_a_signed_path_is_no_chat_app_sign_in(
    hass: HomeAssistant, bridge: Bridge, hass_client_no_auth, hass_access_token: str
) -> None:
    token = hass.auth.async_validate_access_token(hass_access_token)
    signed = async_sign_path(hass, URL, timedelta(minutes=1), refresh_token_id=token.id)
    client = await hass_client_no_auth()
    response = await client.get(signed)
    assert response.status == 401
    assert bridge.seen == []


async def test_home_assistants_discovery_adds_rediscovers_and_removes_the_entry(hass: HomeAssistant) -> None:
    """Drives components/hassio/discovery.py itself; only Supervisor is mocked."""
    await async_setup_component(hass, "http", {})
    message = Discovery(
        addon=SLUG,
        service=DOMAIN,
        uuid=uuidlib.UUID(UUID),
        config={"host": "a0d7b954-ha-nova-relay", "port": 8794, "secret": SECRET},
    )
    supervisor = MagicMock()
    supervisor.addons.addon_info = AsyncMock(return_value=SimpleNamespace(name="HA NOVA"))
    supervisor.discovery.get = AsyncMock(return_value=message)
    with patch.object(hassio_discovery, "get_supervisor_client", return_value=supervisor):
        hassio_discovery.async_setup_discovery_view(hass)
        view = hassio_discovery.HassIODiscovery(hass)

        await view.async_process_new(message)
        await hass.async_block_till_done()
        [entry] = hass.config_entries.async_entries(DOMAIN)
        assert (entry.unique_id, entry.data["host"]) == (UUID, "a0d7b954-ha-nova-relay")

        # Removing only the entry: Home Assistant re-discovers the message.
        await hass.config_entries.async_remove(entry.entry_id)
        await hass.async_block_till_done()
        [entry] = hass.config_entries.async_entries(DOMAIN)

        # The App deletes the message (chat apps off): the entry goes for good.
        supervisor.discovery.get = AsyncMock(side_effect=SupervisorError())
        await view.async_process_del({"service": DOMAIN, "uuid": UUID})
        await hass.async_block_till_done()
        assert hass.config_entries.async_entries(DOMAIN) == []
