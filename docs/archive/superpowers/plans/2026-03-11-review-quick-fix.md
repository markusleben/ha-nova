# Review Quick-Fix Implementation Plan

> Historical plan note: example relay commands in this document predate the Go-first public interface. Current public contract uses `ha-nova relay ...` instead of raw `~/.config/ha-nova/relay` paths.

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After review analysis, detect acute state problems and offer a one-click service-call fix.

**Architecture:** Two edits to existing skill files — a new state-read + output section in Review, and a one-line dispatch clarification in Context Skill.

**Tech Stack:** Markdown (skill definitions only)

> **Note:** Line numbers reference the original `skills/review/SKILL.md`. Since tasks modify the file sequentially, executing agents should locate edit positions by **semantic anchors** (heading names, section labels, surrounding text) rather than relying on line numbers.

---

## Chunk 1: Review Skill Extension

### Task 1: Add state read to Target Resolution

**Files:**
- Modify: `skills/review/SKILL.md:25-57` (Target Resolution section)

- [ ] **Step 1: Add state read instruction after config read**

After Target Resolution step 4 (config read), add step 5:

```markdown
5. Read current state of the primary target entity (for Quick-Fix detection at end of review):
   ```bash
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/<entity_id>"}' \
     | jq 'if .ok then .data.body else empty end' > /tmp/ha-review-state.json
   ```
   If state read fails, continue review — Quick-Fix will be skipped.
```

Insert this after the existing step 4 block (line 57, before the "If config is already in the thread context" paragraph).

- [ ] **Step 2: Verify the edit reads cleanly**

Read the modified file, confirm the new step 5 flows naturally after step 4 and before the "If config is already..." paragraph.

- [ ] **Step 3: Commit**

```bash
git add skills/review/SKILL.md
git commit -m "feat(review): add state read to target resolution for quick-fix detection"
```

---

### Task 2: Adjust scope statement

**Files:**
- Modify: `skills/review/SKILL.md:9-16` (Scope section)

- [ ] **Step 1: Update scope and forbidden line**

Replace lines 9-16:

```markdown
## Scope

Read-only quality review for automations, scripts, and helpers:
- Config quality checks (safety, reliability, performance, style)
- Collision scan (other automations targeting same entities)
- Conflict analysis (real conflicts vs safe patterns)

Forbidden: any write call, any mutation.
```

With:

```markdown
## Scope

Read-only quality review for automations, scripts, and helpers:
- Config quality checks (safety, reliability, performance, style)
- Collision scan (other automations targeting same entities)
- Conflict analysis (real conflicts vs safe patterns)
- Quick-Fix: if an acute state problem is detected, offer a single corrective service call

Read-only analysis. Exception: after explicit user confirmation, one Quick-Fix service call may be executed to correct an acute state problem detected during review.
```

- [ ] **Step 2: Commit**

```bash
git add skills/review/SKILL.md
git commit -m "feat(review): adjust scope to allow quick-fix service call after confirmation"
```

---

### Task 3: Add Quick-Fix step and output section

**Files:**
- Modify: `skills/review/SKILL.md:210-243` (Output Format section)

- [ ] **Step 1: Add Step 4: Quick-Fix Detection after Step 3 (Conflict Analysis)**

Insert before the `## Output Format` heading (line 210):

````markdown
### Step 4: Quick-Fix Detection

After completing Steps 1-3, check if the current entity state (from `/tmp/ha-review-state.json`) shows an acute, fixable problem.

**Qualifies as Quick-Fix:**
- Entity state contradicts automation intent under current conditions (e.g., light `on` when automation should have turned it `off`, climate mode wrong)
- Entity is in error/degraded state that a service call can reset (e.g., `unavailable` cover that needs `cover.stop_cover`)
- Helper value is desynchronized from what automation logic expects (e.g., `input_select` stuck on wrong option)

**Does NOT qualify:**
- State is simply "not what user wants" without clear automation-intent evidence — that's a service-call request, not a review finding
- Fix requires config change (that's a SUGGESTIONS item)
- Multiple equally valid corrections exist (ambiguous — note in SUGGESTIONS instead)
- State read failed or entity unavailable — skip, note: `QUICK_FIX: skipped (state unavailable)`

**If qualified:**
1. Show current state vs expected state
2. Show exact service call that would fix it
3. Ask for natural confirmation (same tier as `ha-nova:service-call` — no token needed, service calls are reversible)

**On confirmation:**
Execute via Relay:
```bash
~/.config/ha-nova/relay core -d '{"method":"POST","path":"/api/services/{domain}/{service}","body":{"entity_id":"{entity_id}",{...service_data}}}'
```
Then verify state changed:
```bash
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/{entity_id}"}'
```
Report result (new state or failure).
````

- [ ] **Step 2: Add QUICK_FIX to Output Format**

In the Output Format section, after the `SUMMARY:` block (around line 243), add:

```markdown
`QUICK_FIX:`
- `none` — no acute state problem detected
- `skipped (state unavailable)` — could not read entity state
- or: current state, expected state, proposed service call, confirmation prompt
```

- [ ] **Step 3: Verify the complete Output Format section lists all 7 sections in order**

Confirm: `REVIEW_MODE`, `CONFIG_FINDINGS`, `COLLISION_SCAN`, `CONFLICTS`, `SUGGESTIONS`, `SUMMARY`, `QUICK_FIX`.

- [ ] **Step 4: Commit**

```bash
git add skills/review/SKILL.md
git commit -m "feat(review): add quick-fix detection step and output section"
```

---

### Task 4: Update Guardrails

**Files:**
- Modify: `skills/review/SKILL.md:244-251` (Guardrails section)

- [ ] **Step 1: Update existing read-only guardrail and add Quick-Fix guardrail**

Replace the existing guardrail line:
```
- Read-only: no writes, no mutations
```
With:
```
- Read-only analysis (exception: Quick-Fix service call after user confirmation)
- Quick-Fix: max 1 service call per review, only after explicit user confirmation, only simple state corrections (no config mutations)
```

- [ ] **Step 2: Commit**

```bash
git add skills/review/SKILL.md
git commit -m "feat(review): add quick-fix guardrail"
```

---

## Chunk 2: Context Skill Dispatch Clarification

### Task 5: Add dispatch note for problem-description intents

**Files:**
- Modify: `skills/ha-nova/SKILL.md:94-122` (Skill Dispatch section)

- [ ] **Step 1: Add clarification after dispatch examples**

After the last dispatch example (line 122, `"Zeige mir die History von Sensor X"`), add:

```markdown

**Problem-description intents** ("X geht nicht", "Y ist falsch", "funktioniert nicht mehr"): dispatch to `ha-nova:review`. Review will analyze the config AND check current entity state — if an acute fix is possible, it offers a Quick-Fix service call at the end.
```

- [ ] **Step 2: Verify the addition doesn't break the ONE-skill-per-intent rule**

The clarification should reinforce that `ha-nova:review` handles everything end-to-end (analysis + optional fix offer). No second skill dispatch needed.

- [ ] **Step 3: Commit**

```bash
git add skills/ha-nova/SKILL.md
git commit -m "feat(context): add problem-description dispatch note for review quick-fix"
```

---

### Task 6: Run dev-sync

- [ ] **Step 1: Sync skill changes to plugin cache**

```bash
bash scripts/dev-sync.sh
```

- [ ] **Step 2: Verify sync succeeded** (no errors in output)

- [ ] **Step 3: Final commit (if dev-sync modified anything tracked)**

Only if dev-sync changes tracked files. Otherwise skip.
