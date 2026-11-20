# 2026-03-17 Cross-Platform Skill Examples Design

## Goal

Make every active HA NOVA skill example teach one shell-agnostic contract that works across macOS, Linux, and Windows client runtimes.

## Constraints

- KISS; no new runtime feature required
- skill files stay concise
- no Python, Node, `cat`, heredocs, `/tmp`, or shell pipes as the primary JSON workflow
- complex relay filters should prefer `--jq-file`
- post-processing saved JSON should prefer `ha-nova relay jq --file`

## Decisions

- Canonical pattern:
  - write payload/filter files with the client's native file tools
  - `ha-nova relay ws --data-file <payload-file>`
  - `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
  - `ha-nova relay ... --out <result-file>`
  - `ha-nova relay jq --file <result-file> '<filter>'`
- keep inline `--jq` only for short, low-risk selectors
- move long `if .ok then .data.body ...` and large array filters to `--jq-file`
- update skill contract tests so the old patterns cannot drift back in
