#!/usr/bin/env bash
#
# lib.sh — shared driver functions for demo recordings.
#
# Drives a REAL interactive `claude` session inside a detached tmux pane while
# asciinema records the pty. Synchronization reads the rendered screen via
# `tmux capture-pane` — never the byte stream — because the fullscreen TUI
# constantly repaints.
#
# Hard rules learned from the calibration spikes (2026-07-13):
# - The tmux pane MUST match the vhs render geometry exactly (see geometry.env).
#   A single column of drift garbles the fullscreen-TUI replay.
# - Turn completion = busy indicator gone + two identical consecutive captures.
#   Content greps alone match the echo of the prompt we typed ourselves.
set -euo pipefail

DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC2034  # consumed by the record-*.sh scripts that source this library
ARTIFACTS_DIR="${DEMO_ARTIFACTS:-$(cd "$DEMO_DIR/../.." && pwd)/.artifacts/demo}"
WORKSPACE="${DEMO_WORKSPACE:-$HOME/smart-home}"
SESSION="${DEMO_TMUX_SESSION:-ha-nova-demo}"
BUSY_PATTERN="esc to interrupt"

# shellcheck source=geometry.env
source "$DEMO_DIR/geometry.env"

require_tools() {
  local missing=()
  for t in tmux asciinema vhs ffmpeg gif2webp python3 jq; do
    command -v "$t" >/dev/null 2>&1 || missing+=("$t")
  done
  if ((${#missing[@]})); then
    echo "ERROR: missing tools: ${missing[*]} — see docs/reference/demo-recording.md" >&2
    exit 1
  fi
}

# --- workspace ---------------------------------------------------------------

setup_workspace() {
  mkdir -p "$WORKSPACE"
  cp "$DEMO_DIR/workspace-template/CLAUDE.md" "$WORKSPACE/CLAUDE.md"
  # The literal name .claude/settings.local.json is gitignored in this repo,
  # so the template ships under a neutral name and is materialized here.
  cp "$DEMO_DIR/workspace-template/claude-settings.json" "$WORKSPACE/demo-settings.json"
  mkdir -p "$WORKSPACE/.claude"
  cp "$DEMO_DIR/workspace-template/claude-settings.json" "$WORKSPACE/.claude/settings.local.json"
}

# Demo sessions run on latest Sonnet with high reasoning effort — configured
# declaratively in workspace-template/claude-settings.json. DEMO_MODEL is an
# ad-hoc override knob (e.g. DEMO_MODEL=haiku for cheap driver tests).
claude_cmd() {
  local model="${DEMO_MODEL:-}"
  local cmd="claude --settings demo-settings.json --strict-mcp-config"
  [[ -n "$model" ]] && cmd+=" --model $model"
  echo "$cmd"
}

# --- tmux session ------------------------------------------------------------

start_session() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x "$DEMO_COLS" -y "$DEMO_ROWS"
  # Silence the "focus-events off" hint claude prints otherwise.
  tmux set -t "$SESSION" focus-events on
  trap 'tmux kill-session -t "$SESSION" 2>/dev/null || true' EXIT
}

kill_session() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
}

screen() { tmux capture-pane -pt "$SESSION"; }

# Visible pane plus scrollback — long turns scroll earlier content (e.g. the
# Preview card) out of the 30-row window.
screen_history() { tmux capture-pane -pt "$SESSION" -S -200; }

dump_screen() {
  echo "--- screen dump ($1) ---" >&2
  screen >&2
  echo "--- end dump ---" >&2
}

# --- input -------------------------------------------------------------------
# Timing windows (typing cadence, reading holds, the /exit cut point) are NOT
# logged wall-clock: asciinema's cast clock drifts against the wall clock, so
# recordings capture input events instead (`rec --capture-input`) and
# compress-cast.py derives every window from the "i" events on the cast's own
# clock.

# Type text with a brisk but human cadence (15-40 ms jitter per keystroke).
type_text() {
  local text="$1" i ch
  for ((i = 0; i < ${#text}; i++)); do
    ch="${text:$i:1}"
    tmux send-keys -t "$SESSION" -l "$ch"
    sleep "0.0$((RANDOM % 3 + 1))$((RANDOM % 10))"
  done
}

submit() {
  sleep 0.5
  tmux send-keys -t "$SESSION" Enter
}

# NOTE: TUI scrolling (PageUp/PageDown) is NOT usable for reading holds —
# the fullscreen TUI auto-jumps back to the bottom on every repaint (verified;
# the "Jump to bottom" toast). Instead, the final answer is shown at its real
# streaming pace: compress-cast.py protects everything after the last submit
# as an "answer" window, and the viewer reads along as it appears.

# --- synchronization ---------------------------------------------------------

# Wait until a pattern appears on screen. Args: pattern, timeout-seconds.
wait_for_screen() {
  local pattern="$1" timeout="${2:-60}" i
  for ((i = 0; i < timeout; i++)); do
    screen | grep -qF "$pattern" && return 0
    sleep 1
  done
  dump_screen "timeout waiting for: $pattern"
  return 1
}

# Wait until claude's turn is over: busy indicator gone AND the screen stable
# for two consecutive captures. Permission dialogs (some command shapes force
# manual approval regardless of the allowlist) are confirmed automatically —
# in the compressed rendering they are a blink, and an honest one.
# Args: timeout-seconds.
wait_idle() {
  local timeout="${1:-240}" i prev cur
  for ((i = 0; i < timeout; i++)); do
    cur="$(screen)"
    if grep -qF "Do you want to proceed?" <<<"$cur"; then
      tmux send-keys -t "$SESSION" -l "1"
      prev=""
      sleep 1
      continue
    fi
    if ! grep -qF "$BUSY_PATTERN" <<<"$cur"; then
      if [[ -n "${prev:-}" && "$cur" == "$prev" ]]; then
        return 0
      fi
      prev="$cur"
    else
      prev=""
    fi
    sleep 1
  done
  dump_screen "timeout waiting for idle"
  return 1
}

# Assert that a pattern is on screen right now; dump and fail otherwise.
expect_on_screen() {
  local pattern="$1"
  if ! screen | grep -qF "$pattern"; then
    dump_screen "expected on screen: $pattern"
    return 1
  fi
}

# Assert that a pattern appeared at some point (visible pane or scrollback).
expect_in_history() {
  local pattern="$1"
  if ! screen_history | grep -qF "$pattern"; then
    dump_screen "expected in history: $pattern"
    return 1
  fi
}

# Skills may ask clarifying questions via a native select menu (tabs, options,
# a Submit step). Walk it by always picking option 1 — the skill's Recommended
# choice. Multi-select tabs toggle on "1" and need an explicit Tab to advance;
# single-select questions advance by themselves (observed live).
answer_menu_recommended() {
  local i scr
  for ((i = 0; i < 10; i++)); do
    scr="$(screen)"
    if grep -qF "Ready to submit" <<<"$scr"; then
      tmux send-keys -t "$SESSION" -l "1"
      sleep 2
      return 0
    fi
    if grep -qF "Enter to select" <<<"$scr"; then
      tmux send-keys -t "$SESSION" -l "1"
      sleep 1.5
      if screen | grep -qF "[✔]"; then
        tmux send-keys -t "$SESSION" Tab
        sleep 1.5
      fi
      continue
    fi
    return 0 # menu gone (question flow finished or never existed)
  done
  dump_screen "menu did not converge"
  return 1
}

# --- recording ---------------------------------------------------------------

# Launch `asciinema rec -c claude ...` inside the pane and wait for the input
# prompt. Args: cast-path.
start_recording() {
  local cast="$1"
  mkdir -p "$(dirname "$cast")"
  tmux send-keys -t "$SESSION" \
    "cd $WORKSPACE && asciinema rec -f asciicast-v2 --capture-input --overwrite -c '$(claude_cmd)' $cast" Enter
  # Status line reads "... · ← for agents" in every permission mode (the
  # "? for shortcuts" variant appears only sometimes) — wait for the stable part.
  wait_for_screen "for agents" 90
  sleep 2 # let startup notices settle before the first keystroke
}

# End the claude session and wait for asciinema to finalize the cast file.
# compress-cast.py cuts the tail at the "/exit" input-event cluster (the
# per-keystroke echo never paints "/exit" as one string in the output).
# Args: cast-path.
end_recording() {
  local cast="$1" i
  type_text "/exit"
  sleep 1
  tmux send-keys -t "$SESSION" Enter
  for ((i = 0; i < 30; i++)); do
    [[ -s "$cast" ]] && screen | grep -qF "Recorded to" && return 0
    sleep 1
  done
  [[ -s "$cast" ]] # accept a written cast even if the notice scrolled away
}
