## Hermes Live SSH Validation

Date: 2026-04-17

### Scope

Validate the real Hermes Linux install path on a live remote host without adding any host-specific SSH details to the repository.

### Plan

1. Copy the current worktree to a temporary remote directory.
2. Run the Hermes local-skill installer from that temporary checkout inside a login shell, so the remote Hermes binary is discoverable in `PATH`.
3. Verify the installed Hermes bundle shape and namespaced sub-skill output under `~/.hermes/skills/ha-nova`, with sub-skill directory names matching their `name:` values.
4. Verify the repo `dev-sync` path refreshes Hermes correctly on Linux.
5. Remove the temporary remote checkout after validation.

### Exit Criteria

- `bash scripts/onboarding/install-local-skills.sh hermes` succeeds on a real remote Hermes host.
- The installed Hermes bundle contains the expected HA NOVA root skill and required sub-skills.
- Hermes sub-skills keep namespaced frontmatter such as `ha-nova-review`.
- `bash scripts/dev-sync.sh` refreshes Hermes successfully on that host.
- No SSH host/path details are committed to the repo.
