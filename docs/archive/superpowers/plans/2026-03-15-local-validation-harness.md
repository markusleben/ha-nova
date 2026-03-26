# Local Validation Harness Plan

1. Add a contract test for one local validation harness entrypoint.
2. Add `scripts/dev/start-local-validation-harness.sh`.
3. Document it in `package.json` and `docs/releasing.md`.
4. Verify with:
   - `npx vitest run tests/onboarding/desktop-validation-contract.test.ts`
   - `bash -n scripts/dev/start-local-validation-harness.sh`
   - `npm run verify`
5. Run code review until no findings remain.
