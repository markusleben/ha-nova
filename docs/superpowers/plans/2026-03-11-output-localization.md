# Output Localization & UX Polish Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Localize all user-facing skill output — section headers, severity labels, finding descriptions — to the user's language with emoji severity and descriptive titles instead of internal codes.

**Architecture:** Central localization rule in Context Skill + output format rewrites in review, write, and helper skills. Pure markdown edits, no code.

**Tech Stack:** Markdown (skill definitions only)

> **Note:** Line numbers reference original files at plan creation time. Since tasks modify files sequentially, executing agents MUST locate edit positions by **semantic anchors** (heading names, surrounding text) rather than line numbers.

---

## Chunk 1: Central Localization Rule

### Task 1: Add Output Localization section to Context Skill

**Files:**
- Modify: `skills/ha-nova/SKILL.md` — insert new section after "Response Format" (after line 92, before "## Skill Dispatch")

- [ ] **Step 1: Insert Output Localization section**

Find the line `Keep orchestration details internal on normal success paths.` (end of Response Format section). Insert after it, before `## Skill Dispatch (Critical)`:

```markdown
## Output Localization (Critical)

All user-facing output MUST follow these rules:
- **Language**: Localize all section headings and labels to the user's language. Use idiomatic phrasing, not literal translations.
- **Severity**: 3 levels only — 🔴 (high/critical) 🟠 (medium) 🟡 (low/info). No text severity labels needed — the emoji is sufficient.
- **Finding titles**: Each finding gets a short descriptive title (2-5 words) explaining WHAT the issue is. Example: "Template-Fallback fehlt", not "R-01".
- **Internal codes**: Check codes (R-01, S-01, H-01, M-01, P-01, F-01, etc.) are for YOUR analysis reference only. NEVER show them in user-facing output.
- **Consistency**: Same sections, same order, every time. The user must recognize the structure across reviews.
```

- [ ] **Step 2: Verify the new section sits between "Response Format" and "Skill Dispatch"**

Read the file and confirm the section order is: Response Format → Output Localization → Skill Dispatch.

- [ ] **Step 3: Commit**

```bash
git add skills/ha-nova/SKILL.md
git commit -m "feat(context): add output localization rules for all skills"
```

---

## Chunk 2: Review Skill Output Format

### Task 2: Rewrite Review Output Format section

**Files:**
- Modify: `skills/review/SKILL.md` — replace Output Format section (starting at `## Output Format`)

- [ ] **Step 1: Replace the entire Output Format section**

Find the section starting with `## Output Format` and ending before `## Guardrails`. Replace with:

```markdown
## Output Format

Return exactly these 7 sections, in this order, every time. Localize all headings to the user's language (see `skills/ha-nova/SKILL.md` → Output Localization).

**Section 1 — Review target:**
- domain (automation / script / helper) and target entity_id

**Section 2 — Findings:**
- numbered list or "no issues found"
- each: `🔴|🟠|🟡 Descriptive title — explanation + fix suggestion`
- 🔴 = high/critical, 🟠 = medium, 🟡 = low/info
- title must describe WHAT the issue is (2-5 words), NOT an internal code

**Section 3 — Collision check:**
- list the checked entity names
- short result: how many related automations/scripts found

**Section 4 — Conflicts:**
- numbered conflicts or "none"
- each: entity_id, what this automation does vs what the other does, risk description
- 🔴 = real conflict, 🟠 = potential, 🟡 = info (safe pattern)

**Section 5 — Suggestions:**
- concrete improvement ideas
- each: short title + what it does + why it helps
- or "none"

**Section 6 — Summary:**
- one-paragraph natural language summary
- mention total findings count and highest severity emoji
- if clean: localized equivalent of "Config looks clean — no issues detected."

**Section 7 — Instant help:**
- if no acute state problem: localized "not needed"
- if state read failed: localized "skipped (state unavailable)"
- if fixable problem detected: current state, expected state, proposed service call, confirmation prompt
```

- [ ] **Step 2: Update inline references to old section keys in review flow**

The review flow body text still references hardcoded English keys. Update these:

**a) Trace Analysis section** — find:
```
4. Include trace-based findings in `CONFIG_FINDINGS` with prefix `T-` (e.g., `T-01: Condition blocked execution in last 3 runs`)
```
Replace with:
```
4. Include trace-based findings in the Findings section with a descriptive title (e.g., `🔴 Bedingung blockiert — Condition wurde in den letzten 3 Runs nie erfüllt`)
```

**b) Step 2 Collision Scan** — find:
```
5. If `related_items_found: 0`, set `CONFLICTS: none` and skip Step 3.
```
Replace with:
```
5. If no related items found, report "no conflicts" in the Conflicts section and skip Step 3.
```

**c) Step 4 Quick-Fix Detection** — find:
```
- State read failed or entity unavailable — skip, note: `QUICK_FIX: skipped (state unavailable)`
```
Replace with:
```
- State read failed or entity unavailable — skip, note in Instant Help section: localized "skipped (state unavailable)"
```

**d) Step 4 Quick-Fix Detection** — find:
```
- Fix requires config change (that's a SUGGESTIONS item)
- Multiple equally valid corrections exist (ambiguous — note in SUGGESTIONS instead)
```
Replace with:
```
- Fix requires config change (that's a Suggestions item)
- Multiple equally valid corrections exist (ambiguous — note in Suggestions section instead)
```

- [ ] **Step 3: Verify the 7 sections are listed in order and the Guardrails section follows unchanged**

Read the file from Output Format through Guardrails. Confirm structure.

- [ ] **Step 4: Commit**

```bash
git add skills/review/SKILL.md
git commit -m "feat(review): rewrite output format with localization, emoji severity, descriptive titles"
```

---

## Chunk 3: Review Agent Template & Reference Doc

### Task 2b: Update Review Agent Template output format

**Files:**
- Modify: `skills/ha-nova/agents/review-agent.md` — replace Output Format section

- [ ] **Step 1: Replace the Output Format section**

Find the section starting with `## Output Format (Structured Text)` (contains `Return exactly these sections:` followed by `REVIEW_MODE:`, `CONFIG_FINDINGS:`, etc.). Replace the entire section (from `## Output Format` to end of file) with:

```markdown
## Output Format

Follow the output format defined in `skills/review/SKILL.md` → Output Format. Same 7 sections (without Instant Help — not applicable to post-write agent reviews), same order. Localize per `skills/ha-nova/SKILL.md` → Output Localization.

For post-write reviews, Section 1 (Review target) must include `mode: post-write`.
```

- [ ] **Step 2: Commit**

```bash
git add skills/ha-nova/agents/review-agent.md
git commit -m "feat(review-agent): replace duplicated output format with reference to review skill"
```

---

### Task 2c: Update skill-architecture.md Post-Write Review Standard

**Files:**
- Modify: `docs/reference/skill-architecture.md` — update Post-Write Review Standard format

- [ ] **Step 1: Replace the output format block**

Find the block:
```
   Focus on CRITICAL/HIGH. Report MEDIUM/LOW as advisory.
```
Replace with:
```
   Focus on 🔴 findings. Report 🟠🟡 findings as advisory.
```

- [ ] **Step 2: Replace the Post-Write Review template**

Find the block:
```
4. Output format (MUST appear in every post-write response):
   ```
   ## Post-Write Review
   **Config Findings:** {CRITICAL/HIGH findings with fix suggestions, or "Clean — no issues found."}
   **Collision Scan:** {conflicts or "No conflicts detected."}
   **Advisory:** {MEDIUM/LOW findings, or omit if none}
   ```
```

Replace with:
```
4. Output format (MUST appear in every post-write response) — localize headings per `skills/ha-nova/SKILL.md` → Output Localization:
   - **Findings**: 🔴🟠🟡 findings with descriptive titles + fix suggestions, or localized "no issues found"
   - **Collision check**: conflicts or localized "no conflicts"
   - **Advisory**: 🟠🟡 findings, or omit if none
```

- [ ] **Step 3: Commit**

```bash
git add docs/reference/skill-architecture.md
git commit -m "feat(docs): align post-write review standard with localized output format"
```

---

### Task 2d: Update contract test for review-agent output format

**Files:**
- Modify: `tests/skills/ha-nova-contract.test.ts` — update assertions for review-agent output keys

- [ ] **Step 1: Replace old output key assertions**

Find and replace lines 129-131 and 137-139 (the 6 hardcoded key assertions):

```typescript
    expect(review).toContain("CONFIG_FINDINGS:");
    expect(review).toContain("COLLISION_SCAN:");
    expect(review).toContain("CONFLICTS:");
```
and:
```typescript
    expect(review).toContain("REVIEW_MODE:");
    expect(review).toContain("SUGGESTIONS:");
    expect(review).toContain("SUMMARY:");
```

Replace all 6 with:
```typescript
    expect(review).toContain("Output Format");
    expect(review).toContain("Output Localization");
    expect(review).toContain("mode: post-write");
```

- [ ] **Step 2: Run the contract test**

```bash
cd /Users/markus/Daten/Development/Privat/ha-nova && npx vitest run tests/skills/ha-nova-contract.test.ts
```

Expected: PASS (all assertions green).

- [ ] **Step 3: Commit**

```bash
git add tests/skills/ha-nova-contract.test.ts
git commit -m "test(contract): update review-agent assertions for localized output format"
```

---

## Chunk 4: Write & Helper Post-Write Review

### Task 3: Update Write Skill Post-Write Review format


**Files:**
- Modify: `skills/write/SKILL.md` — replace Post-Write Review response format (Phase 4, step 4)

- [ ] **Step 1: Replace the response format block in Phase 4**

Find the block:
```
4. Response MUST include this exact structure:
   ```
   ## Post-Write Review
   **Config Findings:** {CRITICAL/HIGH findings with fix suggestions, or "Clean — no issues found."}
   **Collision Scan:** {conflicts with related automations/scripts, or "No conflicts detected."}
   **Advisory:** {MEDIUM/LOW findings, or omit section if none}
   ```
```

Replace with:
```
4. Response MUST include a Post-Write Review section with localized headings (see `skills/ha-nova/SKILL.md` → Output Localization):
   - **Findings**: 🔴🟠🟡 findings with descriptive titles + fix suggestions, or localized "no issues found"
   - **Collision check**: conflicts with related automations/scripts, or localized "no conflicts"
   - **Advisory**: 🟠🟡 findings, or omit if none
```

- [ ] **Step 2: Update the dedup note in Phase 4 step 2**

Find the line:
```
   - **Dedup**: findings from Phase 2 Step 3b that user saw MUST NOT repeat. Example: if R-05 (mode not explicit) was shown pre-write and user proceeded, do not report R-05 again.
```

Replace with:
```
   - **Dedup**: findings from Phase 2 Step 3b that user saw MUST NOT repeat. Track by check type (not code — codes are internal), e.g. if "mode not explicit" was shown pre-write and user proceeded, do not report it again.
```

- [ ] **Step 3: Update Phase 2 Step 3b static checks format**

Find the line:
```
     CRITICAL/HIGH → inline warning with fix suggestion. MEDIUM/LOW → advisory below preview. Clean → skip.
```

Replace with:
```
     🔴 findings → inline warning with fix suggestion. 🟠🟡 findings → advisory below preview. Clean → skip.
```

- [ ] **Step 4: Update Phase 2 Step 3b tracking line**

Find the line:
```
     Track reported finding codes (e.g. R-05, P-01) for dedup in Phase 4 — user proceeding past a warning = implicit ack.
```

Replace with:
```
     Track reported findings by check type for dedup in Phase 4 — user proceeding past a warning = implicit ack.
```

- [ ] **Step 5: Commit**

```bash
git add skills/write/SKILL.md
git commit -m "feat(write): align post-write review with localized output format"
```

---

### Task 4: Update Helper Skill Post-Write Review format

**Files:**
- Modify: `skills/helper/SKILL.md` — replace Post-write review response format (step 3)

- [ ] **Step 1: Replace the response format block**

Find the block:
```
3. Response MUST include:
   ```
   ## Post-Write Review
   **Config Findings:** {H-check findings with fix suggestions, or "Clean — no issues found."}
   **Collision Scan:** {referencing automations/scripts, or "No references found."}
   **Advisory:** {MEDIUM/LOW findings, or omit if none}
   ```
```

Replace with:
```
3. Response MUST include a Post-Write Review section with localized headings (see `skills/ha-nova/SKILL.md` → Output Localization):
   - **Findings**: 🔴🟠🟡 findings with descriptive titles + fix suggestions, or localized "no issues found"
   - **Collision check**: referencing automations/scripts, or localized "no references found"
   - **Advisory**: 🟠🟡 findings, or omit if none
```

- [ ] **Step 2: Commit**

```bash
git add skills/helper/SKILL.md
git commit -m "feat(helper): align post-write review with localized output format"
```

---

## Chunk 5: Sync & Verify

### Task 5: Run dev-sync and verify

- [ ] **Step 1: Sync skill changes to plugin cache**

```bash
bash scripts/dev-sync.sh
```

- [ ] **Step 2: Verify sync succeeded** (no errors in output)

- [ ] **Step 3: Squash commit (if dev-sync modified tracked files)**

Only if dev-sync changes tracked files. Otherwise skip.
