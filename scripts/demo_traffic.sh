#!/usr/bin/env bash
# demo_traffic.sh: Generates gentle background traffic (2 RPS) for live Grafana dashboards
set -euo pipefail

TARGET_URL="${TARGET_URL:-http://localhost:8080}"
echo "Starting continuous background demo traffic against ${TARGET_URL} (Ctrl+C to stop)..."

while true; do
  curl -s -o /dev/null "${TARGET_URL}/healthz" || true
  sleep 0.5
done
