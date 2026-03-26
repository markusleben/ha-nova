# 2026-03-17 Cross-Platform Skill Examples

1. Audit all skill/reference files for inline shell-specific JSON handling.
2. Rewrite affected examples to the file-based relay contract.
3. Tighten skill contract tests for:
   - no Python/Node post-processing
   - no `cat`/heredoc snapshot guidance
   - `--jq-file` on complex filters
   - `relay jq --file` for saved JSON post-processing
4. Re-run focused skill contracts, then full verify.
5. Re-review the skill-doc delta.
