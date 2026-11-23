#!/usr/bin/env bash
set -euo pipefail

echo "Resetting demo events and flushing Redis cache..."
docker exec -i orderflow-postgres-1 psql -U orderflow -d orderflow < scripts/seed_demo.sql
docker exec -i orderflow-redis-1 redis-cli FLUSHALL > /dev/null

echo "Showcase events seeded successfully! Refresh your browser."
