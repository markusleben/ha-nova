# Automation Delete Module

Use blocks:
- `B0_ENV` + `B3_ID_RESOLVE` + `B7_RENDER_DOMAIN_PREVIEW`
- `B8_CONFIRM_TOKEN` + `B9_APPLY_WRITE` + `B10_VERIFY_WRITE`

## Fast-Pass

`FP_AUTOMATION_DELETE = B0+B3+B7+B8+B9+B10`

## Verification

- Delete status `200`/`204` is success by default.
- Optional verify-absent read-back when needed.
