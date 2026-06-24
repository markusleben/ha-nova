# Notification Copy Preservation Spec

## Goal

Prevent HA NOVA write flows from silently rewriting user-facing notification
copy during unrelated automation or script updates.

## Decisions

- Treat notification titles, messages, templates, metadata, and actionable
  payloads as user-authored content.
- Preserve notification copy exactly unless the user explicitly asks for
  notification wording or notification behavior changes.
- Keep notification copy changes visible in `ha-nova diff`; a count-only action
  array change is not enough when an existing notification message changed too.
- Keep the Relay unchanged. This is skill and CLI preview behavior only.

## Verification

- Skill contracts assert the preserve-by-default rule.
- CLI diff tests assert notification message changes remain visible when the
  action list length changes.
- Run focused skill and CLI tests, then full verification before release.
