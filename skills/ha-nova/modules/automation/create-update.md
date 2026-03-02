# Automation Create/Update Module

Use blocks:
- `B0_ENV` + `B1_STATE_SNAPSHOT` + `B2_ENTITY_RESOLVE` + `B3_ID_RESOLVE`
- `B4_BP_GATE` + `B5_BUILD_AUTOMATION` + `B7_RENDER_DOMAIN_PREVIEW`
- `B8_CONFIRM_TOKEN` + `B9_APPLY_WRITE` + `B10_VERIFY_WRITE`

## Fast-Pass

`FP_AUTOMATION_CREATE_UPDATE = B0+B1+B2+B3+B4+B5+B7+B8+B9+B10`

## Output Fields

- `Automation Name`
- `Automation Goal`
- `Entities Used`
- `Behavior Summary`
- `Next Step`

## Constraints

- Target <= 6 relay calls in normal path.
- No exploratory schema-probing loops unless API returns real schema error.
