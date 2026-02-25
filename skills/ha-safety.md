---
name: ha-safety
description: Core write-safety rules for HA Nova operations.
---

# HA Safety (Core)

## Rule 1: Preview Before Execute

For every write operation:
- Show target entities/config.
- Show exact action payload.
- Ask for explicit confirmation.

No confirmation -> no write.

## Rule 2: No Guessing

Never infer unknown entity IDs, service names, or automation IDs.

When uncertain:
1. Query entities/services first.
2. Present candidates.
3. Ask user to pick one.

## Rule 3: Explain Failures Clearly

Return structured failure summary:
- what failed
- why it failed
- next concrete fix step
