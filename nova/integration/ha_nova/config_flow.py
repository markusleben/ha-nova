"""HA NOVA: the entry comes only from the NOVA App's Supervisor discovery."""

import re
from typing import Any

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult
from homeassistant.helpers.service_info.hassio import HassioServiceInfo

DOMAIN = "ha_nova"
_HOST = re.compile(r"^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$")
_SECRET = re.compile(r"^[0-9a-f]{64}$")


class HaNovaConfigFlow(ConfigFlow, domain=DOMAIN):
    """No user step: the owner consented when turning chat apps on."""

    VERSION = 1

    async def async_step_hassio(
        self, discovery_info: HassioServiceInfo
    ) -> ConfigFlowResult:
        """Store the bridge address and secret the App announced."""
        config = discovery_info.config
        host, port, secret = config.get("host"), config.get("port"), config.get("secret")
        if not (
            isinstance(host, str)
            and _HOST.fullmatch(host)
            and type(port) is int
            and 0 < port < 65536
            and isinstance(secret, str)
            and _SECRET.fullmatch(secret)
        ):
            return self.async_abort(reason="invalid_discovery_info")
        data: dict[str, Any] = {
            "host": host,
            "port": port,
            "secret": secret,
            "slug": discovery_info.slug,
        }
        # The unique ID is the discovery uuid: Home Assistant removes the
        # entry when the App deletes that message (chat apps turned off).
        await self.async_set_unique_id(discovery_info.uuid)
        self._abort_if_unique_id_configured(updates=data)
        for entry in self._async_current_entries():
            if entry.data.get("slug") != discovery_info.slug:
                return self.async_abort(reason="already_configured")
            # A new message from the same App (Home Assistant missed the
            # removal of the old one) replaces it: one entry, current secret.
            return self.async_update_reload_and_abort(
                entry,
                unique_id=discovery_info.uuid,
                data=data,
                reason="already_configured",
            )
        return self.async_create_entry(title="HA NOVA", data=data)
