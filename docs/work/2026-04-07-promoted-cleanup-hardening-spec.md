## 2026-04-07 Promoted Cleanup Hardening

- Split active promoted run output from stale artifact prefixes.
- Remove the dashboard lifecycle validator waiver for an expected first config-read miss.
- Make the lifecycle scenario deterministic: create dashboard, save initial config, then verify.
- Keep cleanup protection explicit for caller-provided output directories.
