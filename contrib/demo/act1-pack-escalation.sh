#!/usr/bin/env bash
# act1-pack-escalation.sh — Act 1: Progressive Pack Escalation
#
# Demonstrates: Three manual config edits, three capability levels.
# The presenter edits city.toml live in a visible editor pane. The daemon
# live-reconciles on each save via fsnotify.
#
# Progression:
#   Step 1: city.toml as-is — wasteland-feeder only, no rigs, no agents.
#   Step 2: Presenter uncomments the [[rigs]] block → coder pool + merger.
#   Step 3: Presenter changes "swarm-lifecycle" → "lifecycle" → polecats + refinery.
#
# The script sets up all infrastructure (packs, rig beads, demo repo)
# then opens a tmux layout with city.toml in an editor. The presenter
# drives the demo by editing and saving.
#
# All agents are bash scripts — no Claude API calls.
#
# Usage:
#   ./act2-lifecycle-packs.sh
#
# Env vars:
#   DEMO_CITY      — city directory (default: ~/demo-city)
#   GC_SRC         — gascity source tree (default: /data/projects/gascity)
#   DOLTHUB_TOKEN  — DoltHub API token (wasteland-poll needs this for remote mode)
#   EDITOR         — editor for city.toml (default: nano, vim also works)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GC_SRC="${GC_SRC:-/data/projects/gascity}"
# shellcheck source=narrate.sh
source "$SCRIPT_DIR/narrate.sh"

DEMO_CITY="${DEMO_CITY:-$HOME/demo-city}"
DEMO_SESSION="gc-lifecycle"
EDIT="${EDITOR:-nano}"

# Wasteland poll automation needs DOLTHUB_TOKEN for remote DoltHub API access.
if [[ -z "${DOLTHUB_TOKEN:-}" ]]; then
    WL_ENVRC="/data/projects/wasteland/.envrc"
    if [[ -f "$WL_ENVRC" ]]; then
        # shellcheck source=/dev/null
        source "$WL_ENVRC"
    fi
fi
export DOLTHUB_TOKEN="${DOLTHUB_TOKEN:-}"

# ── Act 1 ─────────────────────────────────────────────────────────────────

narrate "Act 1: Simple to Advanced" --sub "Three edits to city.toml — the daemon does the rest"

echo "  You will edit city.toml live. The daemon watches for changes."
echo ""
echo "  Step 1: Uncomment includes line — wasteland-feeder joins the global pool"
echo "  Step 2: Uncomment the [[rigs]] block — coder pool + merger appear"
echo "  Step 3: Change swarm-lifecycle → lifecycle — polecats + refinery"
echo ""
pause

# ── Clean slate ───────────────────────────────────────────────────────────
# Stop ALL demo cities and sessions — a previous act may still be running.

for city in "$DEMO_CITY" "$HOME/demo-city" "$HOME/eks-demo"; do
    [ -d "$city" ] && (cd "$city" && gc stop 2>/dev/null) || true
done
for sess in gc-lifecycle gc-provider gc-eks; do
    tmux kill-session -t "$sess" 2>/dev/null || true
done

# Kill bd idle-monitors first (they respawn dolt servers if killed alone).
pkill -f "bd dolt idle-monitor" 2>/dev/null || true
# Kill any stale dolt servers that would block port 3307.
pkill -f "dolt sql-server" 2>/dev/null || true
# Wait for port 3307 to be released.
for i in $(seq 1 10); do
    ss -tlnp 2>/dev/null | grep -q ':3307 ' || break
    sleep 1
done

rm -rf "$DEMO_CITY"

# ── Set up all infrastructure upfront ─────────────────────────────────────
# We initialize everything (packs, rig, beads) so the presenter only
# needs to edit city.toml — no commands to run during the demo.

step "Setting up infrastructure (packs, rig, beads)..."

# BEADS_DOLT_AUTO_START=0 prevents gc init / gc rig add from spawning their
# own dolt server + idle-monitor on port 3307. gc start will start the city
# dolt later with the correct data-dir. The beads init that fails here will
# be re-done by gc start (we delete metadata.json to force re-init).
export BEADS_DOLT_AUTO_START=0

# Initialize from lifecycle example (gives us the base structure).
gc init --from "$GC_SRC/examples/lifecycle" "$DEMO_CITY"

# Copy additional packs locally (remote fetch doesn't resolve formula
# layers yet, so automations need local paths to be discovered).
cp -r "$GC_SRC/examples/swarm-lifecycle/packs/swarm-lifecycle" "$DEMO_CITY/packs/"
cp -r "$GC_SRC/examples/wasteland-feeder" "$DEMO_CITY/packs/"

# Clone demo repo (lifecycle pack needs push/pull).
DEMO_REPO="$DEMO_CITY/demo-repo"
git clone -q https://github.com/gastownhall/gc-demo-repo "$DEMO_REPO"

# Register the rig (sets up routes, hooks, pack config).
# Beads init will fail (no dolt running, auto-start disabled) — that's fine,
# gc start handles it. The || true catches the expected beads init error.
(cd "$DEMO_CITY" && gc rig add "$DEMO_REPO" --include packs/lifecycle) || true

# Remove partial metadata so gc start's runBdInit re-initializes from scratch
# on the city dolt (correct data-dir: .gc/dolt-data/).
rm -f "$DEMO_CITY/.beads/metadata.json"
rm -f "$DEMO_REPO/.beads/metadata.json"

unset BEADS_DOLT_AUTO_START

step "Rig infrastructure ready (routes, hooks, pack)"

# ── Bootstrap city.toml for init phase ─────────────────────────────────
# Write a minimal config with the rig but NO pack includes. This lets
# gc start initialize dolt + rig beads without starting agents that would
# consume the pre-seeded beads. After seeding, we overwrite with the
# staged demo version.

DEMO_REPO_ABS=$(cd "$DEMO_REPO" && pwd)

cat > "$DEMO_CITY/city.toml" <<EOF
[workspace]
name = "demo-city"

[[rigs]]
name = "demo-repo"
path = "$DEMO_REPO_ABS"
includes = []

[daemon]
patrol_interval = "1s"
EOF

# ══════════════════════════════════════════════════════════════════════════
# SCREEN LAYOUT
# ══════════════════════════════════════════════════════════════════════════
#
# ┌─────────────────────────────┬─────────────────────────────┐
# │                             │                             │
# │  city.toml (editor)         │  gc status (watch -n3)      │
# │                             │                             │
# │  The file you edit live.    │  Agents appear/disappear    │
# │  Save → daemon reconciles.  │  as you edit and save.      │
# │                             │                             │
# ├─────────────────────────────┼─────────────────────────────┤
# │                             │                             │
# │  gc events --follow         │  Controller (foreground)    │
# │                             │                             │
# │  Real-time event stream     │  gc start --foreground      │
# │  showing reconciliation.    │  reconciliation output.     │
# │                             │                             │
# └─────────────────────────────┴─────────────────────────────┘
#
# Left = cause (config edits, events). Right = effect (status, agents).
# ══════════════════════════════════════════════════════════════════════════

step "Creating 4-pane tmux layout..."
echo ""
echo "  ┌──────────────────────┬──────────────────────┐"
echo "  │ city.toml (editor)   │ gc status (live)     │"
echo "  ├──────────────────────┼──────────────────────┤"
echo "  │ gc events --follow   │ Controller           │"
echo "  └──────────────────────┴──────────────────────┘"
echo ""

PANE_EDITOR=$(tmux new-session -d -s "$DEMO_SESSION" -x 200 -y 50 -P -F "#{pane_id}")
PANE_STATUS=$(tmux split-window -h -t "$PANE_EDITOR" -P -F "#{pane_id}")
PANE_EVENTS=$(tmux split-window -v -t "$PANE_EDITOR" -P -F "#{pane_id}")
PANE_CTRL=$(tmux split-window -v -t "$PANE_STATUS" -P -F "#{pane_id}")

tmux select-pane -t "$PANE_EDITOR" -T "city.toml"
tmux select-pane -t "$PANE_STATUS" -T "Status"
tmux select-pane -t "$PANE_EVENTS" -T "Events"
tmux select-pane -t "$PANE_CTRL" -T "Controller"

tmux set-option -t "$DEMO_SESSION" pane-border-status top
tmux set-option -t "$DEMO_SESSION" pane-border-format "#{pane_title}"

# Pane top-right: gc status (refreshes every 3s).
tmux send-keys -t "$PANE_STATUS" \
    "cd $DEMO_CITY && watch -n3 'gc status 2>/dev/null || echo \"Starting...\"'" C-m

# Pane bottom-left: Events stream.
tmux send-keys -t "$PANE_EVENTS" \
    "cd $DEMO_CITY && gc events --follow" C-m

# Pane bottom-right: Controller (foreground — stays alive, watches config).
# Pass DOLTHUB_TOKEN so the wasteland-poll automation can access the remote API.
tmux send-keys -t "$PANE_CTRL" \
    "export DOLTHUB_TOKEN='$DOLTHUB_TOKEN' && cd $DEMO_CITY && gc start --foreground" C-m

# Wait for gc start to bring up the dolt server + init beads.
step "Waiting for dolt server..."
for i in $(seq 1 30); do
    ss -tlnp 2>/dev/null | grep -q ':3307 ' && break
    sleep 1
done

# Wait for rig beads database to be initialized by gc start.
# Port 3307 means dolt is listening, but the rig database ("dr") is created
# by runBdInit during gc start's beads lifecycle — a few seconds after dolt.
step "Waiting for beads database..."
for i in $(seq 1 30); do
    (cd "$DEMO_REPO" && bd list >/dev/null 2>&1) && break
    sleep 1
done

# ── Seed beads (dolt is now running via gc start) ─────────────────────────
# Pre-seed so work is waiting when agents come online.
# Separate beads for each pack so the demo shows distinct work per step.

step "Seeding work beads..."

# Swarm-lifecycle beads (Step 2: coders pick these up)
(cd "$DEMO_REPO" && bd create "Fix parser edge case" --labels pool:demo-repo/coder 2>/dev/null) || true
(cd "$DEMO_REPO" && bd create "Update API documentation" --labels pool:demo-repo/coder 2>/dev/null) || true
(cd "$DEMO_REPO" && bd create "Add input validation" --labels pool:demo-repo/coder 2>/dev/null) || true

# Lifecycle beads (Step 3: polecats pick these up)
(cd "$DEMO_REPO" && bd create "Add authentication module" --labels pool:demo-repo/polecat 2>/dev/null) || true
(cd "$DEMO_REPO" && bd create "Implement rate limiting middleware" --labels pool:demo-repo/polecat 2>/dev/null) || true
(cd "$DEMO_REPO" && bd create "Add health check endpoint" --labels pool:demo-repo/polecat 2>/dev/null) || true

step "6 beads seeded (3x pool:demo-repo/coder for swarm, 3x pool:demo-repo/polecat for lifecycle)"

# ── Write staged city.toml ────────────────────────────────────────────────
# Overwrite with the staged demo version. The daemon detects the change via
# fsnotify and reconciles — lifecycle agents stop, rig is removed from config.
# Bead data persists in dolt. When the presenter uncomments the rig,
# agents will find the pre-seeded beads waiting.

cat > "$DEMO_CITY/city.toml" <<'OUTER'
# ── GAS CITY DEMO ─────────────────────────────────────────────────────────
#
# This file drives the demo. Edit it, save, and watch the daemon respond.
#
# Step 1: Uncomment the includes line below.
# Step 2: Uncomment the [[rigs]] block below.
# Step 3: Change "swarm-lifecycle" → "lifecycle" in the includes line.
# ──────────────────────────────────────────────────────────────────────────

[workspace]
name = "demo-city"
# ── STEP 1: Uncomment the line below to join the global inference pool ───
# includes = ["packs/wasteland-feeder"]

[daemon]
patrol_interval = "1s"

# Override remote pack automation interval for demo speed.
[[automations.overrides]]
name = "wasteland-poll"
interval = "10s"

# ── STEP 2: Uncomment the 4 lines below to add a project ────────────────
OUTER

# Inject the dynamic repo path
cat >> "$DEMO_CITY/city.toml" <<EOF
# [[rigs]]
# name = "demo-repo"
# path = "$DEMO_REPO_ABS"
# includes = ["packs/swarm-lifecycle"]
#
# ── STEP 3: Change "swarm-lifecycle" → "lifecycle" above ─────────────────

# ── Uncomment after Step 3 (lifecycle pack defines the polecat agent) ──
# [[patches.agent]]
# dir = ""
# name = "polecat"
# [patches.agent.env]
# DOLTHUB_TOKEN = "\$DOLTHUB_TOKEN"
EOF

# Wait for daemon to reconcile the config change (stop lifecycle agents).
sleep 3

step "city.toml written with staged variants"
echo ""

# ── Show the city.toml ────────────────────────────────────────────────────

step "city.toml contents:"
echo ""
cat "$DEMO_CITY/city.toml"
echo ""

# Pane top-left: Open city.toml in the editor.
tmux send-keys -t "$PANE_EDITOR" \
    "$EDIT $DEMO_CITY/city.toml" C-m

# Select the editor pane so the presenter's cursor is there.
tmux select-pane -t "$PANE_EDITOR"

step "Demo ready. Attaching to tmux session..."
echo ""
echo "  INSTRUCTIONS:"
echo ""
echo "  Step 1: Uncomment the includes line."
echo "          Save. Wasteland-feeder joins the global inference pool."
echo ""
echo "  Step 2: Uncomment the [[rigs]] block (4 lines)."
echo "          Save. Watch the status pane — coder pool + merger appear."
echo ""
echo "  Step 3: Change 'swarm-lifecycle' to 'lifecycle' in the editor."
echo "          Save. Watch coders disappear, polecats + refinery appear."
echo ""
echo "  Detach: Ctrl-b d"
echo ""
pause "Press Enter to attach..."

tmux attach-session -t "$DEMO_SESSION"

# ── Teardown ──────────────────────────────────────────────────────────────

(cd "$DEMO_CITY" && gc stop 2>/dev/null) || true
tmux kill-session -t "$DEMO_SESSION" 2>/dev/null || true

# ── Done ──────────────────────────────────────────────────────────────────

narrate "Act 1 Complete" --sub "Three edits, zero to Gas Town"

echo "  Step 1: Uncomment includes line           → wasteland-feeder, global pool"
echo "  Step 2: Uncomment [[rigs]] block         → coder pool + merger"
echo "  Step 3: swarm-lifecycle → lifecycle       → branches + refinery"
echo ""
echo "  Same beads. Same daemon. Each step: edit, save, watch."
echo ""
pause "Press Enter to continue to next act..."
