# Runbook: Redis Fail-Open Active

## Symptom
`RedisFailOpenActive` is firing. Redis operations (rate limiting, sold-out cache) are failing open to PostgreSQL due to errors or timeouts.

## Likely Causes
1. Redis instance crashed or restarted.
2. Network timeout between `api`/`worker` and Redis (> 1s timeout exceeded).
3. Redis memory exhausted and rejecting writes with OOM.

## Diagnostic Queries & Commands
- **PromQL:** `sum by (operation) (rate(orderflow_redis_failopen_total[5m]))`
- **Redis ping:** `docker compose exec redis redis-cli ping`
- **Redis logs:** `docker compose logs redis --tail=50`
- **Redis memory:** `docker compose exec redis redis-cli info memory`

## Mitigation
1. Test Redis connectivity: `docker compose exec redis redis-cli ping` (expected output: `PONG`).
2. If Redis is down, restart it: `docker compose restart redis`.
3. Check Redis memory usage: if `maxmemory` is reached, verify eviction policy (`allkeys-lru` or `volatile-lru`).
4. OrderFlow automatically falls back to PostgreSQL, preventing outages, but database load may increase while Redis is down.
