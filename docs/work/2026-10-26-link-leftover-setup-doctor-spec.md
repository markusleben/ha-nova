# A leftover Home Assistant Link: setup explains it, doctor runs anyway (2026-10-26)

Status: active

Follow-up to #813 (Link paused in setup). Works with `2026-10-24-link-cleanup-for-me-spec.md` (0.28): this spec covers what setup and doctor say and offer; that spec covers what `cloud remove` does when it can't check the sign-in.

## Problem

Seen on 2026-10-26: an older install, updated with `ha-nova update`, had an unfinished Home Assistant Link from an earlier version.

- Setup stayed in the terminal and never said why: Link that isn't ready turns the setup page off (`setupPageCloudOK`).
- Setup printed one line ("Home Assistant Link isn't finished. Setup goes on without it."). After the update, the verify step needed a new key, so setup stopped with "First finish or remove Home Assistant Link with a command above". Nothing said what Link is, that it's left over, or what each command does.
- Doctor stopped at the transport choice before any check. With route `automatic` and a reinstalled NOVA App (new Relay instance id) it printed "local Relay returned an unsupported health response; no Cloud fallback or functional request was attempted" and exited. Doctor had no Link line, and a key-read error told the user to "run: ha-nova doctor" from inside doctor.

## Setup

When: a plain interactive `ha-nova setup` (no flags, no `--service`, the default server) whose saved Link isn't ready, on a setup that has a home-network connection (not Link only). Paired (hybrid) setups get the same choice; their Finish is the resume #813 offered. `--non-interactive` is unchanged.

In a terminal (input and output are a TTY), before anything else:

> Home Assistant Link on this computer isn't finished. It's left over from an earlier HA NOVA setup. HA NOVA still works at home without it.
> › Finish Link now (sign in to Home Assistant once)
>   Remove Link from this computer — HA NOVA keeps working at home.
>   Not now — Setup goes on. It stops if it needs to change this computer's key.

- A safety hold: the first line is "HA NOVA paused Link to keep your home safe.", then "HA NOVA still works at home without it." There's no Finish.
- A removal that didn't finish: "Removing Home Assistant Link didn't finish. To finish removing: ha-nova cloud remove --server default. HA NOVA still works at home without it."
- Enter picks Finish when it's offered, otherwise Not now. Remove only runs when chosen.
- Finish is offered only in a desktop session (where the browser sign-in can run) and not while an interrupted removal is pending (add and reconnect refuse then).
- **Finish** runs `ha-nova cloud reconnect` (or `add`, as the printed command says) in this process. **Remove** runs `ha-nova cloud remove` in this process without its second "Remove?" question: choosing Remove is that answer. It's still a person at a terminal, so the 0.28 "Clean up for me" offer applies there unchanged.
- After either one, setup reads the settings again and goes on with what's saved now: Link ready or gone → a normal setup; still unfinished → the paused path below; a hold → the hold's steps, exit 1.
- **Not now**: the hold's steps and exit 1, as before; otherwise the paused path.
- Ctrl+C, at the choice or inside Finish (for example at its pairing-code prompt): the commands below, then setup ends as on any Ctrl+C.
- A command that leaves Link still unfinished goes on as Not now, without asking again.

Without a TTY, setup prints that explanation and "Setup goes on without it. You'll see what to do at the end." A paired setup prints the explanation, then its resume offer or commands as before. A hold prints its steps and exits 1, as before.

Paused path (as #813): no credential changes. The commands now say what each does:

>   To finish it, run: ha-nova cloud reconnect --server default
>     You sign in to Home Assistant once. Then HA NOVA works away from home too.
>   To remove it, run: ha-nova cloud remove --server default
>     Link is removed from this computer. HA NOVA keeps working at home.

When a step needs a new key: those commands, then "Setup needs to change this computer's key, and it won't while Link is unfinished. Choose above, then run ha-nova setup again." Exit 1.

When the setup page would otherwise open: "Link isn't finished, so setup runs here in the terminal." (under a hold: "Link is paused, so setup runs here in the terminal.").

After a reinstalled App: `cloud remove` keeps a paired profile's saved Relay id. When setup has checked this computer's key and no Link is saved, it drops that id, as a fresh local pairing does, because the check doesn't prove the Relay's Link identity. The next `cloud add` reads the id again from the App, so remove → setup → add works.

Shared-access installs: pairing refuses while Link is saved, so neither setup nor doctor offers the switch to this computer's own access then; the paused commands say to switch after Link is removed.

## Doctor

- The transport choice never ends doctor. If the saved route fails (a Link problem, or the automatic route's local check), doctor prints the reason in plain words, marks the run failed, and checks Home Assistant, the NOVA App, and the AI apps over the home connection. A Link-only setup has no home connection: doctor says why it can't check the App and still checks the AI apps. The App check gets its own time limit, so a slow Link attempt can't use it up.
- A failing key read (missing, locked) skips the App check instead of ending doctor; it also fails the run.
- A Relay identity mismatch (the App answers with another instance id than the one saved for Link) gets its own cause and the text: "The NOVA App isn't the one this computer's Link was set up with (was it reinstalled?). Remove Link: ha-nova cloud remove --server <name>. Then connect this computer again: ha-nova setup. Add Link again after that if you want it." Setup drops the stale id (above), so `cloud add` alone would fail the same way. Functional commands, `cloud status`, and Finish use the same words. A health reply without a Relay id stays an unsupported reply, not a changed identity.
- Whenever the home connection stands in for a failed route (a safety check, an identity mismatch, or Link), its checks are diagnostic only: doctor reads `/health` and nothing else (no WebSocket check, no App update offer).
- A Link line whenever the saved Link isn't ready or is held. It's a warning and doesn't fail doctor:
  - unfinished: "Home Assistant Link isn't finished on this computer. HA NOVA still works at home without it.", then "To finish: …" and "To remove: …";
  - a safety hold: its steps as today (output an AI assistant may read; never the `--confirm` command);
  - any other hold: "Home Assistant Link is paused on this computer." and "To remove: …";
  - a removal that didn't finish: "Removing Home Assistant Link didn't finish. To finish removing: …".
  - When the route problem already named the Link commands, the Link line comes without them.
- Doctor never prints "run: ha-nova doctor".

## Tests

Fakes only, the test keyring file, and the package's test secret dir. No real Home Assistant, Link, or keychain.

- Doctor: Link not ready plus a healthy fake App → exit 0, the Link line with both commands. Identity mismatch → the mismatch text, the remove command, and still the App and AI-app lines; exit 1.
- Setup: Remove runs the remove flow and setup goes on with the new settings. Not now leaves `config.json` byte-identical. Without a TTY: the explanation, then (at the stop) the commands with what each does.
- Second round: Enter under a hold is Not now; Ctrl+C inside Finish ends setup; a paired setup can Remove; remove → setup → add drops the stale Relay id; doctor sends nothing but the health check after a mismatch; mismatch over Link; a reply without a Relay id; a removal that didn't finish; no repeated commands.
