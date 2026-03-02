# Script Create/Update Module

Use blocks:
- `B0_ENV` + `B1_STATE_SNAPSHOT` + `B2_ENTITY_RESOLVE` + `B3_ID_RESOLVE`
- `B6_BUILD_SCRIPT` + `B7_RENDER_DOMAIN_PREVIEW`
- `B8_CONFIRM_TOKEN` + `B9_APPLY_WRITE` + `B10_VERIFY_WRITE`

## Fast-Pass

`FP_SCRIPT_CREATE_UPDATE = B0+B1+B2+B3+B6+B7+B8+B9+B10`

## Output Fields

- `Script Name`
- `Script Goal`
- `Inputs/Variables`
- `Actions/Entities Used`
- `Next Step`
