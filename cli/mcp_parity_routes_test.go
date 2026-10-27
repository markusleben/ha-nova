package main

// The parity table (docs/work/2026-10-25-chat-parity-spec.md → Parity test),
// read by mcp_parity_test.go.

// mcpComputerOnly (items 1 and 5): the commands that set up HA NOVA itself.
// They have no chat route, their flags need no entry, and the header names
// exactly these, in the bullet that says mcpComputerOnlySays.
var mcpComputerOnly = []string{"setup", "pair", "doctor", "update", "check-update", "version", "uninstall", "cloud", "chat-apps", "status", "server", "try"}

const mcpComputerOnlySays = "this sets up HA NOVA on a computer"

// mcpParityExempt: commands that are neither chat-routed nor computer only.
var mcpParityExempt = map[string]string{
	"mcp":   "the chat bridge itself: it serves these tools",
	"serve": "the NOVA Relay App's chat bridge: it serves these tools",
}

// mcpParityRoutes (item 1): every command, subcommand, and flag the skills
// name, and every flag in the usage of a command they use → its chat route,
// "tool[ argument[=value]]" (alternatives joined by " | "), or the reason it
// needs none (its flags then need none either). A flag is looked up under
// its subcommand, then its command.
var mcpParityRoutes = map[string]string{
	"apply": "ha_apply", "apply --plan": "ha_apply plan_id", "apply --confirm": "ha_apply confirm_code", "apply --no-snapshot": "ha_apply no_snapshot",
	"call": "ha_call service", "call --match": "ha_call match", "call --area": "ha_call area", "call --device": "ha_call device",
	"call --set": "ha_call data", "call --data-file": "ha_call data", "call --response": "ha_call response", "call --skill": "ha_call skill",
	"diff": "ha_read endpoint=diff", "diff --before": "ha_read before", "diff --after": "ha_read after", "diff --out": "ha_read endpoint=diff",
	"find": "ha_find phrase", "find --domain": "ha_find domain", "find --area": "ha_find area", "find --state": "ha_find state",
	"find --limit": "ha_find limit", "find --include-disabled": "ha_find include_disabled", "find --aliases": "ha_find aliases",
	"get": "ha_get entity_id", "get --config": "ha_get config", "get --related": "ha_get related", "get --references": "ha_get references",
	"get --dashboard": "ha_get dashboard", "get --dashboards": "ha_get dashboards",
	"history": "ha_history entity_id", "history --since": "ha_history since", "history --until": "ha_history until", "history --logbook": "ha_history logbook",
	"map": "ha_map", "map --section": "ha_map section", "map --entity": "ha_map entity", "map --first-look": "ha_map first_look",
	"map --insights": "ha_map insights", "map --limit": "ha_map limit",
	"plan": "ha_plan kind", "plan --domain": "ha_plan domain", "plan --id": "ha_plan id",
	"plan call": "ha_plan kind=call", "plan call --match": "ha_plan match", "plan call --area": "ha_plan area", "plan call --device": "ha_plan device",
	"plan call --set": "ha_plan data", "plan call --data-file": "ha_plan data", "plan call --response": "ha_plan response", "plan call --skill": "ha_plan skill",
	"plan write": "ha_plan kind=write", "plan write --create": "ha_plan create", "plan write --file": "ha_plan draft", "plan write --base": "ha_plan base",
	"plan write --title": "ha_plan title", "plan write --icon": "ha_plan icon",
	"plan delete": "ha_plan kind=delete", "plan revert": "ha_plan kind=revert", "plan revert --target": "ha_plan id",
	"plan restore": "ha_plan kind=restore", "plan restore --snapshot": "ha_plan snapshot", "plan raw": "ha_plan kind=raw", "plan raw --file": "ha_plan requests",
	"relay": "ha_read | ha_plan kind=raw", "relay ws": "ha_read endpoint=ws | ha_plan kind=raw", "relay core": "ha_read endpoint=core | ha_plan kind=raw",
	"relay files": "ha_read endpoint=files | ha_plan kind=raw", "relay backups": "ha_read endpoint=backups | ha_plan kind=raw",
	"relay supervisor":  "ha_read endpoint=supervisor | ha_plan kind=raw",
	"relay health":      "every result carries notices[]: the session bootstrap is skipped",
	"relay --data-file": "ha_read payload | ha_plan requests", "relay --body-file": "ha_read payload | ha_plan requests",
	"relay --method": "ha_read method | ha_plan requests", "relay --path": "ha_read path | ha_plan requests",
	"relay --out": "ha_read", "relay --out-binary": "ha_read endpoint=camera", "relay --strict-status": "ha_read",
	"relay --jq": "ha_read jq", "relay --jq-file": "ha_read jq", "relay jq": "ha_read jq", "relay jq --file": "ha_read jq",
	"relay jq -r": "results are JSON already: no raw-string flag", "relay jq -c": "results are JSON already: no compact flag",
	"relay jq -e": "the header tells the model to check for null or false itself",
	"snapshot":    "ha_read endpoint=snapshot", "snapshot show": "ha_read action=show", "snapshot verify": "ha_read action=verify",
	"snapshot --list": "ha_read list", "snapshot --target": "ha_read target", "snapshot --domain": "ha_read domain", "snapshot --against": "ha_read against",
	"snapshot --server": "a chat connection is pinned to one home",
	"snapshot save":     "ha_apply", "snapshot save --data-file": "ha_apply", // apply saves a helper update's checkpoint itself
	"trace latest": "ha_trace action=latest", "trace list": "ha_trace action=list", "trace get": "ha_trace action=get",
	"trace --json": "ha_trace", "trace --server": "a chat connection is pinned to one home",
}
