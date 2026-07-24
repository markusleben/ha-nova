# Session Bootstrap

Apply this contract before the first Home Assistant task in a session,
regardless of which HA NOVA skill was loaded directly.

## Check

1. Before the selected server profile's first Home Assistant or HA NOVA task,
   and before any relay command, run `ha-nova check-update --quiet`.
   SessionStart context and background
   `--quiet --json` refreshes do not replace this human-output check because
   they cannot carry the pending census question.
2. Run one check for every server profile used in this agent session. For a
   non-default profile, apply the same `HA_NOVA_SERVER=<name>` selection used
   by the task. After the first profile check, scope
   `HA_NOVA_NO_CENSUS=1` to each additional profile's check so switching
   servers cannot duplicate the install-wide pending census notice. Remember a
   completed
   profile check even when it is empty or fails.
3. Remember complete HA NOVA update blocks (`Update available`, `Return to
   stable`, `HA NOVA update available`, or `HA NOVA return to stable`,
   regardless of capitalization), Relay blocks (`Relay outdated` or `Relay
   update available`), and `CENSUS ASK PENDING`.
4. Continue the requested task even when the advisory check is empty or fails;
   either outcome counts as this session's one attempt. An update notice never
   replaces or interrupts the user's request. Empty output stays silent.
5. Treat later `[ha-nova]` notices from relay commands as the same session
   status. Never repeat a notice already surfaced in this session.

## HA NOVA Update Callout

After the requested result, surface an available HA NOVA update exactly once
as a separate localized callout:

- installed version → latest version
- up to three supplied highlight lines
- supplied release URL
- offer `ha-nova update`

Surface the HA NOVA update and census callouts at most once for the whole
agent session, even after a server-profile switch. Never update without
consent. After a successful `ha-nova update`, tell the user to start a new
AI-client session so the refreshed skills take effect.

## Relay Update Callout

After the requested result, surface a Relay warning exactly once per selected
server profile as a separate localized callout:

- For the Home Assistant NOVA Relay App, name the installed and available
  versions when supplied, say that the Relay restarts, and ask whether to
  prepare the guided update. A yes is only permission to invoke
  `ha-nova:updates`; that skill must show its App-update preview and obtain
  confirmation before installing with a partial backup.
- For a standalone Container/Core Relay, do not offer a guided install. Point
  to the manual image pull and container recreation instead.

## Census Consent Choice

Surface `CENSUS ASK PENDING` exactly once per session as a standalone,
localized privacy choice after the requested result. Apply the context skill's
Interactive Choices contract: use a native selectable menu when available and
the identical numbered fallback otherwise.

Never stack this choice with another active menu, write preview, runtime-action
confirmation, destructive confirmation code, or user-assisted readiness
question. Remember the pending notice and defer it to the next conflict-free
response. A deferred machine notice does not count as a presentation.
Immediately before a conflict-free presentation, run
`ha-nova census notice-presented`. Render the choice only when its output
starts with `CENSUS NOTICE PRESENT`; render nothing when it returns
`CENSUS NOTICE SKIP` or fails. The census choice must be the only active choice
in that response and must close it; print nothing after its options.

The disclosure must preserve all of these distinctions:

- Before asking, explain in friendly, non-pressuring language that by
  contributing, the user helps the maintainer get a rough picture of how many
  installations participate, which HA NOVA and Relay versions they use, and
  how operating systems are distributed. This helps prioritize compatibility
  work, tests, bug fixes, and new features where they are likely to help most.
  Present this as a directional planning input, not a roadmap vote or feature
  promise; participation remains optional.
- If the user agrees, HA NOVA sends the first report now; further reports are
  sent no sooner than seven days later.
- The fixed JSON body contains only the payload schema, a dedicated random
  Census installation ID, HA NOVA version, operating system, and a recently
  observed Relay version when available. The ID only lets the same
  participating installation count once. It is not derived from or reused
  from a hardware or device identifier, pairing, a user, a Relay, or Home
  Assistant; HA NOVA attaches no device data. No usage or Home Assistant data
  is sent.
- Cloudflare is the hosting provider for the census endpoint. It processes the
  JSON plus the source IP and connection metadata of the same HTTPS request
  under its privacy policy.
- HA NOVA ingest code does not read or store the source IP.
- In visible plain language, say that counts are private maintainer
  statistics and are voluntary, self-reported participating installations,
  not verified people or the complete installed base.

Render that disclosure as one compact heading plus at most five short lines.
Use those five lines for: purpose and planning value; cadence; exact JSON field
categories plus no usage/Home Assistant data; random-ID counting and origin;
Cloudflare processing, HA NOVA source-IP non-reading/non-storage, and the
voluntary-count limitation. Combine related clauses without dropping any
distinction.
Do not paste the reference text or expose the machine-directed notice. Put the
three actions immediately after the disclosure. Reserve technical terms such
as "attempt" and "application JSON body" for the details view; the visible
choice says "message content (JSON)" and "no sooner than seven days later".

Offer exactly three short, localized effects:

1. **Yes — contribute**: run `ha-nova census choose <choice-id> yes`.
2. **No — do not contribute**: run `ha-nova census choose <choice-id> no`.
3. **Show exact data**: run `ha-nova census status`, display the literal JSON
   object verbatim without omitting or renaming fields, then state the
   Cloudflare transport disclosure, change no consent state, and render the
   same three choices again with the same choice ID.

`<choice-id>` is the exact `cns-choice-...` value returned by
`ha-nova census notice-presented`. Never replace a displayed choice ID with
unbound `ha-nova census on|off`; the choice ID prevents an old UI action from
overwriting newer consent.

Explain the value without guilt, pressure, or recommending opt-in. If a client
requires a default or recommended option, use the privacy-safe No choice. The
selected Yes or No is the single consent;
never ask for a second confirmation. Report the stored choice only after the
command succeeds, and distinguish a saved opt-in from an unconfirmed first
report. If a choose command says the choice is stale, report that it did not
change current consent and inspect `ha-nova census status`; never retry with
an unbound command.

If `ha-nova census status` fails, name the error, explicitly state that consent
is unchanged, and immediately re-render the same three choices.

A missing, dismissed, free-form, or ambiguous answer runs nothing and changes
nothing. The immediate re-render after **Show exact data** is part of the same
choice interaction. Otherwise, do not surface the prompt again unsolicited in
the same session. The CLI claims this one-time choice before rendering;
interruption may safely leave it unanswered rather than risk repeated privacy
prompts. Never infer opt-in from memory, configuration, or unrelated
agreement.

For details use `docs/reference/census.md`; preserve command names exactly.
