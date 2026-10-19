"""custom_components/ha_nova links to nova/integration/ha_nova."""

from pathlib import Path
import sys

import pytest

import custom_components

# No __pycache__ inside nova/integration/ha_nova: the App copies that folder.
sys.dont_write_bytecode = True

# The plugin's own testing_config/custom_components package comes first on
# sys.path; add ours next to it.
custom_components.__path__.append(str(Path(__file__).parent / "custom_components"))


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    """Let Home Assistant load custom_components/ha_nova."""
