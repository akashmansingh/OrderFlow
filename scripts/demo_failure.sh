#!/usr/bin/env bash
# demo_failure.sh: Simulates worker failure, outbox stagnation alert firing, and automatic recovery
set -euo pipefail

TARGET_URL="${TARGET_URL:-http://localhost:8080}"
DB_URL="${DATABASE_URL:-postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable}"

echo "========================================================"
echo "      ORDERFLOW RESILIENCE & FAILURE SIMULATION DEMO    "
echo "========================================================"

echo "Step 1: Stopping asynchronous saga worker..."
docker compose stop worker

echo "Step 2: Submitting reservation traffic while worker is offline..."
# Creates an event and a reservation to queue outbox events
go run ./cmd/verify -database-url "$DB_URL" || true

echo "Step 3: Waiting 35 seconds for StuckSagas / OutboxStagnation alert..."
sleep 35

echo "Step 4: Restarting saga worker..."
docker compose start worker

echo "Step 5: Waiting 10 seconds for worker to drain outbox backlog..."
sleep 10

echo "Step 6: Verifying all database invariants hold..."
go run ./cmd/verify -database-url "$DB_URL"

echo "========================================================"
echo "DEMO COMPLETED: System recovered cleanly with 0 oversells"
echo "========================================================"
