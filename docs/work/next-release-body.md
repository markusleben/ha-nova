# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Moved out of the README's "What's Next" section on 2026-10-21. Each item moves into the README or the release notes only when it is in a stable release.

## In the next release (0.28)

- **Chat apps can do what the computer can.** In Claude or ChatGPT you can rename devices, move them to rooms, set up helpers, scenes, dashboards, and integrations, edit files, read the error log, and look at a camera, with the same preview and confirmations as on a computer. Only setting up HA NOVA itself (setup, pairing, updates, turning chat apps on or off) stays on a computer.
- **Claude stops asking before every step, if you let it.** The setup page and the NOVA page say what to set in Claude: with a linked phone, every HA NOVA tool on "Always allow"; without one, all but "Run a confirmed change".
- **HACS installs and updates, and dashboard resources, now take your own confirmation, like a delete,** in chat apps and on a computer: that code runs in every signed-in browser.
- **A leftover Home Assistant Link is explained.** Setup says what it is and lets you finish or remove it right there; `ha-nova doctor` runs all its checks anyway and shows both ways out.

Release-prep todos for these:

- `README.md:175`: the "Always allow" advice by phone, as in `docs/reference/chat-apps.md`; `README.md:182`: drop "Helpers, dashboards, and files still need HA NOVA on a computer" (only setting up HA NOVA itself stays there). Then remove the `needs-computer` README entry from the allow list in `tests/docs/canonical-wording.test.ts`.
- hanova.app, after the tag: `index.html:478` (the same advice) and `demo/index.html:88` (the demo has no phone: the no-phone variant).

## Planned

- **Deeper reviews everywhere**: the full 40+ rule check, for more than automations
- **Community skills**: write a new workflow as a Markdown file and share it with everyone
- **A guided Docker path in setup**: Container and Core setup without the manual steps
