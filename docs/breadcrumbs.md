# Breadcrumbs

## 2026-03-04

### Session: Review Agent + Skill Polish

**Commit:** `c3d0ae6` on main (not pushed, no PR yet)

**What was done:**
- Built `review-agent.md` — post-write quality reviewer with 27 BP checks + collision scan + 3-step conflict analysis
- Integrated as Phase 4 in write skill (advisory, post-apply)
- Added standalone review/analyze route to router skill
- Cleaned up legacy artifacts (backup mechanism, LLAT_SERVICE, old skill names)
- Improved entity discovery (language-agnostic search, area→search/related flow)
- Fixed config-ID fallback (slug → unique_id) across all skills
- Install script now requires explicit target (no default)
- All 140 tests pass, skills installed to all 3 targets

**Live-tested with sub-agents (5 scenarios):**
1. Script listing (36 scripts, 1 call)
2. Script config read with domain crossover (5 calls, slug→unique_id fallback worked)
3. Area-based search Wohnzimmer (5 calls, search/related flow)
4. Script trace debugging (4 calls, traces analyzed)
5. No-match Bewässerung (6 calls, correct escalation)

**Review agent was reviewed by:**
- Code quality agent (all clean, test gap fixed)
- Domain accuracy agent (4 fixes applied: S-03, R-05/R-06, R-03, area safe pattern)

**Still uncommitted (separate from this commit):**
- README.md, banner.svg, package.json, CHANGELOG.md, social-preview.png
- .claude/INSTALL.md, .codex/INSTALL.md, .npmignore
- scripts/onboarding/bin/ha-nova
- .gemini/, .opencode/ dirs

**Next steps:**
- Push commit or create PR when ready
- Consider live-testing review-agent against a real automation (standalone mode)
- Remaining release tasks: GitHub release + npm publish (Task #18)
