# Runbook: Availability SLO Error Budget Burn Rate

## Symptom
`SLOAvailabilityBurnRatePage` (1h window burn rate > 14.4x) or `SLOAvailabilityBurnRateTicket` (6h window burn rate > 6x) is firing. The service is burning its 99.9% availability error budget at an unsustainable rate.

## Likely Causes
1. Upstream database failures causing 500 Internal Server Errors on reservation or auth endpoints.
2. Connection pool exhaustion on PostgreSQL or Redis.
3. Regression in application logic after a recent deployment.

## Diagnostic Queries & Commands
- **PromQL (Error Rate by Path):** `sum by (path, status) (rate(orderflow_http_requests_total{status=~"5.."}[5m]))`
- **PromQL (Current Error Ratio):** `job:orderflow_http_errors:ratio5m`
- **Application logs:** `docker compose logs --tail=100 -f api | grep -i "error"`

## Mitigation
1. Identify the failing endpoint from PromQL: check which `path` has the highest 5xx error rate.
2. Check database connectivity: `docker compose exec postgres pg_isready -U orderflow`.
3. Check database connection pool metrics in Prometheus: `pgx_pool_acquired_conns / pgx_pool_max_conns`.
4. If a recent deployment caused the regression, roll back to the previous stable release tag immediately:
   `git checkout <last-stable-tag> && docker compose up -d --build`.
