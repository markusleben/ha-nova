# Relay Token Reuse Wizard Design

## Problem

The current setup wizard handles only:

- existing local token already stored on the same machine
- or generating a brand-new token

It does not cleanly support the common cross-device case:

- Relay already configured in Home Assistant
- user is onboarding a second machine
- user wants to reuse the existing Relay Auth Token instead of rotating it

## Goal

- Make cross-device onboarding first-class on macOS and Windows.
- Keep the wizard minimal.
- Avoid unnecessary token rotation and unnecessary Home Assistant edits.
- Let users move backward when they notice a wrong choice or wrong host/token.

## Decision

Step 2 becomes an explicit token-choice page.

### If a local token already exists

Show masked hint and offer 3 choices:

1. Keep saved token
2. Paste existing token from another device / Home Assistant
3. Generate a new token

### If no local token exists

Show 2 choices:

1. Paste existing token
2. Generate a new token

## UX Rules

### Navigation

- Every interactive step accepts:
  - `back` → go to previous step
  - `exit` → cancel setup cleanly
- Verification retry prompts also accept `back`
- Going back never discards already entered values unless the user overwrites them
- Page transitions remain screen-cleared in interactive TTY mode

### Step order

1. Client choice
2. Home Assistant discovery + address
3. Relay install guidance
4. Token choice / token entry
5. Verification
6. Skill install

- `Paste existing token`:
  - prompt for token input
  - save to secure storage
  - do **not** automatically open the Relay config page
  - continue directly to verification

- `Generate new token`:
  - generate token
  - save to secure storage
  - copy to clipboard when possible
  - open Relay config page
  - ask user to save the new Relay Auth Token in NOVA Relay

- `Keep saved token`:
  - keep current local token
  - continue directly to verification

## Why This Is The Smallest Clean Flow

- Covers first install and second-device install with one page
- Gives users a clean correction path instead of forcing a full rerun
- Avoids hidden assumptions
- Avoids forcing users to touch Home Assistant again when they already have a working Relay token
- Preserves the current secure-storage model

## Scope

- `cli/setup_interactive.go`
- `cli/setup_ui.go`
- focused setup wizard tests
