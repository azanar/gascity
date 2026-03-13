#!/usr/bin/env bash
# act3-eks-scaleup.sh — Act 3: EKS Scale-Up
#
# Demonstrates: Start small, scale huge with one config edit.
#
# Story:
#   Start with 5 agents on EKS working through a modest backlog.
#   "New project approved, new budget approved."
#   Presenter edits pool.max from 5 to 200 in city.toml. Saves.
#   The daemon reconciles. 200 pods materialize. Queue drains.
#
# Screen layout:
#   ┌─────────────────────────────┬─────────────────────────────┐
#   │ Controller logs             │ kubectl get pods -w          │
#   │                             │                              │
#   │ gc start --foreground or    │ Wall of pods materializing   │
#   │ gc daemon logs --follow     │ as pool scales up.           │
#   ├─────────────────────────────┼─────────────────────────────┤
#   │ gc events --follow          │ mcp-mail dashboard           │
#   │                             │                              │
#   │ Events flooding in as       │ Inter-agent mail traffic     │
#   │ agents claim + close beads. │ at scale.                    │
#   ├─────────────────────────────┴─────────────────────────────┤
#   │ Progress: 42/200 complete  ████████░░░░░░  Open: 158     │
#   └───────────────────────────────────────────────────────────┘
#
# Usage:
#   ./act3-eks-scaleup.sh [initial-count] [scale-count]
#
# Args:
#   initial-count  — starting pool size (default: 5)
#   scale-count    — pool size after scale-up (default: 200)
#
# Env vars:
#   GC_HYPERSCALE_MOCK  — "true" for shell mock, no API cost (default: true)
#   ACT3_TIMEOUT        — auto-teardown seconds (default: 300)
#   GC_K8S_NAMESPACE    — K8s namespace (default: gc)
#   GC_SRC              — gascity source tree (default: /data/projects/gascity)
#   EDITOR              — editor for city.toml edits (default: nano)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GC_SRC="${GC_SRC:-/data/projects/gascity}"
# shellcheck source=narrate.sh
source "$SCRIPT_DIR/narrate.sh"

INITIAL="${1:-5}"
SCALE="${2:-200}"
DEMO_CITY="${DEMO_CITY:-$HOME/eks-demo}"
DEMO_SESSION="gc-eks"
ACT3_TIMEOUT="${ACT3_TIMEOUT:-300}"
GC_K8S_NAMESPACE="${GC_K8S_NAMESPACE:-gc}"
GC_HYPERSCALE_MOCK="${GC_HYPERSCALE_MOCK:-true}"
EDIT="${EDITOR:-nano}"

# ── Preflight ─────────────────────────────────────────────────────────────

command -v gc >/dev/null 2>&1 || { echo "ERROR: gc not found in PATH" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl not found in PATH" >&2; exit 1; }
command -v tmux >/dev/null 2>&1 || { echo "ERROR: tmux not found in PATH" >&2; exit 1; }

kubectl get ns "$GC_K8S_NAMESPACE" >/dev/null 2>&1 || {
    echo "ERROR: K8s namespace '$GC_K8S_NAMESPACE' not found" >&2
    exit 1
}

# ── Act 3 ─────────────────────────────────────────────────────────────────

narrate "Act 3: EKS Scale-Up" --sub "$INITIAL agents → $SCALE agents — one config edit"

echo "  Start: $INITIAL agents on EKS, working through a small backlog."
echo "  Then:  New project, new budget. Edit pool.max $INITIAL → $SCALE."
echo "  Watch: $SCALE pods materialize. Queue drains."
echo ""
pause

# ── Clean previous demo ──────────────────────────────────────────────────
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

# ── Build prebaked image ──────────────────────────────────────────────────

step "Building prebaked image: gc-worker:latest"
gc build-image "$GC_SRC/examples/hyperscale" --tag gc-worker:latest
echo ""

# ── Initialize city ──────────────────────────────────────────────────────

# BEADS_DOLT_AUTO_START=0 prevents gc init from spawning its own dolt server
# + idle-monitor on port 3307. gc start will start the city dolt later.
export BEADS_DOLT_AUTO_START=0

step "Initializing EKS city with pool.max = $INITIAL..."
gc init --from "$GC_SRC/examples/hyperscale" "$DEMO_CITY"

unset BEADS_DOLT_AUTO_START

# Write the city.toml with the scale-up commented out.
# The presenter will edit pool.max from $INITIAL to $SCALE live.
cat > "$DEMO_CITY/city.toml" <<EOF
# ── EKS SCALE-UP DEMO ────────────────────────────────────────────────────
#
# Current: $INITIAL workers on EKS.
# To scale: Change pool.max from $INITIAL to $SCALE below. Save.
# The daemon will reconcile and spin up $SCALE pods.
# ──────────────────────────────────────────────────────────────────────────

[workspace]
name = "eks-demo"
start_command = "true"

[[agent]]
name = "worker"
scope = "rig"
prompt_template = "packs/hyperscale/prompts/worker.md.tmpl"
start_command = "{{.ConfigDir}}/scripts/mock-worker.sh"
nudge = "Pick up a bead, process it, close it."
idle_timeout = "5m"

# ── SCALE-UP: Change $INITIAL → $SCALE below ─────────────────────────────
[agent.pool]
min = 0
max = $INITIAL
check = "bd list --label=pool:worker --status=open --json 2>/dev/null | jq length 2>/dev/null || echo 0"

[daemon]
patrol_interval = "10s"
max_restarts = 3
restart_window = "5m"
shutdown_timeout = "5s"
EOF

step "City initialized at $DEMO_CITY"

# Remove partial metadata so gc start re-initializes on the city dolt.
rm -f "$DEMO_CITY/.beads/metadata.json"

# ── Mock mode ─────────────────────────────────────────────────────────────

if [ "$GC_HYPERSCALE_MOCK" = "true" ]; then
    step "Mock mode: workers use shell commands instead of Claude"
    echo ""
fi

pause "Press Enter to start the EKS demo..."

# ══════════════════════════════════════════════════════════════════════════
# SCREEN LAYOUT
# ══════════════════════════════════════════════════════════════════════════

step "Creating 5-pane tmux layout..."
echo ""
echo "  ┌──────────────────────┬──────────────────────┐"
echo "  │ Controller logs      │ kubectl get pods -w   │"
echo "  ├──────────────────────┼──────────────────────┤"
echo "  │ gc events --follow   │ mcp-mail dashboard   │"
echo "  ├──────────────────────┴──────────────────────┤"
echo "  │ Progress: 0/$SCALE → $SCALE/$SCALE                    │"
echo "  └─────────────────────────────────────────────┘"
echo ""

cd "$DEMO_CITY"

PANE_CTRL=$(tmux new-session -d -s "$DEMO_SESSION" -x 200 -y 50 -P -F "#{pane_id}")
PANE_PODS=$(tmux split-window -h -t "$PANE_CTRL" -P -F "#{pane_id}")
PANE_EVENTS=$(tmux split-window -v -t "$PANE_CTRL" -P -F "#{pane_id}")
PANE_MAIL=$(tmux split-window -v -t "$PANE_PODS" -P -F "#{pane_id}")
PANE_PROGRESS=$(tmux split-window -v -t "$PANE_EVENTS" -l 5 -P -F "#{pane_id}")

tmux select-pane -t "$PANE_CTRL" -T "Controller"
tmux select-pane -t "$PANE_PODS" -T "Pods"
tmux select-pane -t "$PANE_EVENTS" -T "Events"
tmux select-pane -t "$PANE_MAIL" -T "Mail"
tmux select-pane -t "$PANE_PROGRESS" -T "Progress"

tmux set-option -t "$DEMO_SESSION" pane-border-status top
tmux set-option -t "$DEMO_SESSION" pane-border-format "#{pane_title}"

# Pane top-right: Pod watch.
tmux send-keys -t "$PANE_PODS" \
    "kubectl -n $GC_K8S_NAMESPACE get pods -w -l gc/component=agent 2>/dev/null || kubectl -n $GC_K8S_NAMESPACE get pods -w" C-m

# Pane middle-left: Events stream.
tmux send-keys -t "$PANE_EVENTS" \
    "cd $DEMO_CITY && gc events --follow" C-m

# Pane middle-right: MCP mail dashboard.
tmux send-keys -t "$PANE_MAIL" \
    "cd $DEMO_CITY && watch -n3 'gc mail inbox --all 2>/dev/null || echo \"No mail yet\"'" C-m

# Pane bottom: Progress counter (starts tracking once scale-up beads exist).
tmux send-keys -t "$PANE_PROGRESS" \
    "cd $DEMO_CITY && $SCRIPT_DIR/progress.sh worker $SCALE" C-m

# Pane top-left: Controller (foreground).
tmux send-keys -t "$PANE_CTRL" \
    "cd $DEMO_CITY && GC_K8S_IMAGE=gc-worker:latest GC_K8S_NAMESPACE=$GC_K8S_NAMESPACE GC_HYPERSCALE_MOCK=$GC_HYPERSCALE_MOCK gc start --foreground" C-m

# Wait for gc start to bring up the dolt server + init beads.
step "Waiting for dolt server..."
for i in $(seq 1 30); do
    ss -tlnp 2>/dev/null | grep -q ':3307 ' && break
    sleep 1
done

# Wait for beads database to be initialized by gc start.
step "Waiting for beads database..."
for i in $(seq 1 30); do
    (cd "$DEMO_CITY" && bd list >/dev/null 2>&1) && break
    sleep 1
done

# ── Seed initial small backlog ────────────────────────────────────────────
# Seed after gc start so dolt is running and the beads database exists.

step "Seeding $INITIAL work beads (initial backlog)..."
for i in $(seq 1 "$INITIAL"); do
    (cd "$DEMO_CITY" && bd create "Initial task $i" --labels pool:worker 2>/dev/null) || true
done
step "$INITIAL beads seeded. The $INITIAL agents will process these."

step "Controller starting with $INITIAL workers..."

# ── Auto-teardown timer ──────────────────────────────────────────────────

(
    sleep "$ACT3_TIMEOUT"
    (cd "$DEMO_CITY" && gc stop 2>/dev/null) || true
    tmux kill-session -t "$DEMO_SESSION" 2>/dev/null || true
) &
TEARDOWN_PID=$!
trap 'kill $TEARDOWN_PID 2>/dev/null || true' EXIT

# ── Narration ─────────────────────────────────────────────────────────────

echo ""
step "PHASE 1: $INITIAL agents processing $INITIAL beads"
echo ""
echo "  Watch the pods pane — $INITIAL workers running."
echo "  Events show them claiming and closing beads."
echo ""
echo "  When ready for the scale-up moment:"
echo ""
echo "  1. In another terminal, seed the big backlog:"
echo "     cd $DEMO_CITY && $SCRIPT_DIR/seed-hyperscale.sh $SCALE $DEMO_CITY"
echo ""
echo "  2. Open city.toml and change pool.max from $INITIAL to $SCALE:"
echo "     $EDIT $DEMO_CITY/city.toml"
echo ""
echo "  3. Save. Watch $SCALE pods materialize."
echo ""
echo "  Auto-teardown in ${ACT3_TIMEOUT}s"
echo ""

tmux attach-session -t "$DEMO_SESSION"

# ── Done ──────────────────────────────────────────────────────────────────

narrate "Act 3 Complete" --sub "$INITIAL → $SCALE pods, one config edit, queue drained"
pause "Press Enter to finish..."
