# Spec: Remove YAML Language Server Headers from Published Winget Manifests

Date: 2026-03-25

## Problem

The release workflow validates the published winget manifest on Windows and fails on warnings.
Generated manifest files still included `# yaml-language-server: $schema=...` headers.
`winget validate` accepted the manifest content but emitted warnings for those headers.

## Decision

Do not emit YAML language-server schema header comments in release-built winget manifests.

## Scope

- Remove schema header comment lines from the winget manifest generator
- Update release contract coverage to assert those headers are absent

## Why

Those comments are editor hints, not required release metadata.
Published manifests must be warning-free under the repo's release contract.
