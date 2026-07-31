# PowerShell 5.1 Relay Unicode Round-Trip Spec

Status: merged — #435; released in v0.21.3
Date: 2026-07-24
Issue: #434

## Goal

Relay JSON file workflows preserve Unicode code points. Unsupported byte
encodings fail before configuration lookup, authentication, or a network
request. CLI-owned text output is deterministic UTF-8 without a BOM.

## Contract

- `--data-file`, `--body-file`, `--jq-file`, and other strict text inputs accept
  UTF-8 with zero or one leading UTF-8 BOM.
- A leading UTF-8 BOM is removed before JSON or jq parsing.
- UTF-16LE, UTF-16BE, UTF-32LE, and UTF-32BE BOMs are rejected with the detected
  encoding named. BOM-less UTF-16 JSON is rejected when its leading JSON bytes
  have the characteristic alternating-NUL form.
- Other invalid UTF-8 is rejected as an unsupported or ambiguous text encoding.
  The error states that no request was sent and gives a PowerShell 5.1-safe
  recovery.
- Bytes that are already valid UTF-8 cannot reveal whether another tool
  previously decoded them incorrectly. Documentation must therefore require an
  explicit UTF-8 decoder when PowerShell 5.1 reads CLI output.
- Relay text responses are validated as UTF-8 before stdout or `--out`.
  `--out` removes one optional response BOM and writes the resulting bytes
  directly, producing UTF-8 without a BOM on every operating system.
- Shell redirection is outside CLI control. Agent guidance prefers `--out` and
  warns that Windows PowerShell 5.1 `>`/`Out-File` writes UTF-16LE.

## PowerShell 5.1 Guidance

For read-modify-write:

1. Retrieve with `ha-nova relay ... --out <file>`.
2. Read bytes, remove at most one UTF-8 BOM explicitly, then decode with
   `System.Text.UTF8Encoding(false, true)`. Do not use `ReadAllText`: its BOM
   detection can override the supplied decoder.
3. Serialize with `ConvertTo-Json`.
4. Write with `System.IO.File.WriteAllText` and
   `System.Text.UTF8Encoding(false)`.
5. Submit with `--data-file` or `--body-file`.

Do not use default `Get-Content`, `Set-Content`, `Out-File`, or `>` as an
encoding boundary for mutation JSON in Windows PowerShell 5.1.

## Verification

- Byte-exact request and `--out` round trip using umlauts/eszett, accented
  Latin, typographic punctuation, Japanese, and emoji.
- Plain UTF-8 and UTF-8 BOM inputs produce identical request bytes.
- UTF-16LE/BE with and without BOM, UTF-32 BOMs, and invalid UTF-8 produce zero
  requests and actionable errors.
- A non-UTF-8 Relay response is never written as text.
- Documentation and agent-contract tests pin the safe PowerShell workflow.
- The Windows job builds and invokes `ha-nova.exe`, checks a fixed expected
  UTF-8 byte sequence, and rejects UTF-16LE/BE BOM files.
