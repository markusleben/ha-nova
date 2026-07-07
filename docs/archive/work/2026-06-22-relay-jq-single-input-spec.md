# Relay JQ Single Input Spec

Status: implemented locally.

## Problem

During review, an agent tried to compare two saved JSON files with
`ha-nova relay jq` and jq `input(s)`. The embedded relay jq runner does not
support multi-input programs, so it failed with:

```text
jq compile error: input(s)/0 is not allowed
```

## MVP Fix

- Keep `ha-nova relay jq` intentionally single-input.
- Document that `input`/`inputs`/multi-file jq programs are unsupported.
- For two-file comparisons, use a native local JSON parser.
- For automation/script draft-vs-readback comparisons, prefer existing
  normalized object comparison helpers where available.

## Reason

This preserves the small Relay CLI contract and avoids pretending to implement
full GNU jq compatibility.
