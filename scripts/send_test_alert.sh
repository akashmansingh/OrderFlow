#!/usr/bin/env bash
# send_test_alert.sh: Fires a synthetic test alert to Alertmanager API v2
set -euo pipefail

ALERTMANAGER_URL="${ALERTMANAGER_URL:-http://localhost:9093}"
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Sending test alert to Alertmanager at ${ALERTMANAGER_URL}..."

curl -s -X POST "${ALERTMANAGER_URL}/api/v2/alerts" \
  -H "Content-Type: application/json" \
  -d "[
    {
      \"labels\": {
        \"alertname\": \"TestManualAlert\",
        \"severity\": \"warning\",
        \"service\": \"orderflow-api\",
        \"instance\": \"manual-test\"
      },
      \"annotations\": {
        \"summary\": \"Manual test alert from OrderFlow verification\",
        \"description\": \"Verifying Alertmanager webhook notifications and routing.\",
        \"runbook_url\": \"docs/runbooks/target-down.md\"
      },
      \"startsAt\": \"${NOW}\"
    }
  ]"

echo ""
echo "Test alert successfully submitted to Alertmanager."
