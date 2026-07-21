#!/usr/bin/env bash
#
# cleanup-hero.sh — delete the demo automation created by a hero take and
# verify it is gone. Run after EVERY take, successful or not.
#
# Resolution path (see skills/ha-nova/relay-api.md, "ID Types & Resolution"):
# entity registry (WS) -> unique_id -> REST DELETE on the config id. There is
# no REST list route for automation configs.
#
# Usage: cleanup-hero.sh ["Alias Prefix"] [--yes]
#
# Prefix matching can catch a user's own automation on a live instance
# ("Pool deck lights evening"), so every delete lists the matched names and
# requires interactive confirmation unless --yes is passed.
set -euo pipefail

PREFIX="Pool deck lights"
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --yes) ASSUME_YES=1 ;;
    *) PREFIX="$arg" ;;
  esac
done

list_matches() {
  # Case-insensitive: the model may title-case the alias ("Pool Deck Lights…").
  # A failed relay lookup must never read as "nothing to clean" — the demo
  # automation would silently survive in the live instance.
  local prefix_lc raw
  prefix_lc=$(tr '[:upper:]' '[:lower:]' <<<"$PREFIX")
  if ! raw=$(ha-nova relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
    -jq "[.data.entities[]
          | select((.ei | startswith(\"automation.\"))
                   and ((.en // \"\") | ascii_downcase | startswith(\"$prefix_lc\")))
          | \"\(.ei)\t\(.en // \"?\")\"]"); then
    echo "ERROR: relay lookup failed — cannot tell whether demo automations remain." >&2
    exit 1
  fi
  ha-nova relay jq -r '.[]' <<<"$raw"
}

list_entity_ids() {
  list_matches | cut -f1
}

mapfile -t matches < <(list_matches)
mapfile -t ids < <(printf '%s\n' "${matches[@]:-}" | cut -f1 | sed '/^$/d')

if ((${#ids[@]} == 0)); then
  echo "No demo automation matching alias prefix \"$PREFIX\" — nothing to clean."
  exit 0
fi

echo "Matched ${#ids[@]} automation(s) by alias prefix \"$PREFIX\":"
printf '  %s\n' "${matches[@]}"
if ((!ASSUME_YES)); then
  read -r -p "Delete ALL of the above? Only demo automations should be listed. [y/N] " answer
  [[ "$answer" == "y" || "$answer" == "Y" ]] || { echo "Aborted — nothing deleted."; exit 1; }
fi

echo "Deleting ${#ids[@]} demo automation(s): ${ids[*]}"
for ei in "${ids[@]}"; do
  uid=$(ha-nova relay ws -d "{\"type\":\"config/entity_registry/get\",\"entity_id\":\"$ei\"}" \
    -jq '.data.unique_id' | ha-nova relay jq -r '.')
  if [[ -z "$uid" || "$uid" == "null" ]]; then
    echo "ERROR: no unique_id for $ei — delete manually." >&2
    exit 1
  fi
  ha-nova relay core -method DELETE -path "/api/config/automation/config/$uid" >/dev/null
  echo "  deleted $ei (config id $uid)"
done

sleep 1
mapfile -t left < <(list_entity_ids)
if ((${#left[@]} > 0)); then
  echo "ERROR: still present after delete: ${left[*]}" >&2
  exit 1
fi
echo "Verified: no demo automation left."
