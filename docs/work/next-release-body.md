# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Fixes

- Setup page, "I use Claude or ChatGPT in the browser": the button now says
  that Home Assistant must be reachable from the internet (Home Assistant
  Link or your own HTTPS address). Right after the owner signs in, setup
  checks that, Home Assistant 2026.10 or newer, and Home Assistant OS or
  Supervised, before it touches the App or connects this computer. If one
  is missing, the page says what, with "Set it up, then check again" or
  "Use HA NOVA on this computer instead".
