# HA NOVA Health Parser Hardening Spec

Status: active

## Goal

Reduce avoidable local parsing errors in `ha-nova:health` runs by making the skill instructions explicit about mixed System Health event shapes, battery detection, and jq quoting.

## Changes

- Treat `system_health/info` event `.data` as mixed type: object, string, number, boolean, null, or absent.
- Inspect `.data.success`, `.data.error`, `.data.info`, or nested fields only after confirming `.data | type == "object"`.
- Treat scalar System Health event payloads as informational/ignored, not parser failures.
- Use `attributes.device_class == "battery"` as the primary low-battery detector.
- Avoid fragile inline jq regex and shell-escaped regex for battery filtering.
- Use `--jq-file` for non-trivial filters and type-check before indexing arrays or objects.

## Verification

- Update the health skill contract test to pin the new parser rules.
- Run the focused health/calendar contract test, docs verification, safe-core, and dev sync.
