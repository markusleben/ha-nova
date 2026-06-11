# Linux Secure Storage Recovery Spec

Date: 2026-04-18
Status: active

## Goal

Make Linux setup recoverable in the GNOME Keyring SSH/headless case when the active Secret Service owner is GNOME Keyring, including the true fresh-init state where no default collection exists yet, without adding insecure storage fallbacks, package-manager automation, or extra public CLI surface.

## Scope

- add one setup-only interactive recovery stage before host/token work
- allow exactly one late persistence retry in the same run
- keep non-interactive setup fail-loud, but append a precise interactive recovery hint for the recoverable class
- split locked-vs-uninitialized Linux secure-storage states so the UX stays truthful
- verify recovery with a real go-keyring probe, not just a Secret Service alias read
- document the release-bound Linux real-machine validation lane

## Non-Goals

- no insecure token storage fallback
- no automatic package installation
- no generic recovery flow for all Linux keyring backends
- no new public command or flag

## Implementation Notes

- detect recoverability only when the active `org.freedesktop.secrets` owner is GNOME Keyring
- prompt for the existing local Linux keyring password only in interactive TTY setup for locked collections
- prompt for a new local Linux keyring password plus confirmation when GNOME Keyring must create the default collection
- zero password bytes after the recovery command returns
- use GNOME Keyring's DBus internal password methods on one live session-bus connection instead of shelling out to `gnome-keyring-daemon --unlock`
- after recovery, rerun saved-token detection and setup-state routing before continuing
- if persistence hits the same recoverable keyring error later in the run, offer one retry only

## Verification

- targeted Go regressions for the recovery stage, late retry, no re-prompt, doctor hint, and non-interactive hint
- full `cd cli && go test -count=1 -timeout 180s ./...`
- Linux real-machine proof on a logged-in desktop user session, including fresh-init and locked-keyring recovery over SSH
- release docs updated with the Linux real-machine onboarding proof lane
