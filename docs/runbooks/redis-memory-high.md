# Runbook: Redis Memory High

## Symptom
`RedisMemoryHigh` is firing. Redis used memory exceeds 85% of allocated maximum memory.

## Likely Causes
1. Cache keys created without expiration TTL.
2. Unexpected growth of rate-limiting keys or token buckets.
3. Insufficient memory limit configured for the workload.

## Diagnostic Queries & Commands
- **PromQL:** `redis_memory_used_bytes / redis_memory_max_bytes`
- **Redis memory info:** `docker compose exec redis redis-cli info memory`
- **Key count by DB:** `docker compose exec redis redis-cli info keyspace`

## Mitigation
1. Check if keys have TTL: run `docker compose exec redis redis-cli scan --count 20` and check `TTL <key>`.
2. Ensure all cache keys set by OrderFlow have TTL (e.g., `CACHE_SOLD_OUT_TTL`).
3. If memory is exhausted, flush disposable cache keys: `docker compose exec redis redis-cli eval "return redis.call('del', unpack(redis.call('keys', 'orderflow:cache:*')))" 0`.
