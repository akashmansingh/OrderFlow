# Runbook: AbsentMetric

## Symptom
`absent(orderflow_http_requests_total)` evaluated to true for > 2 minutes.

## Likely Causes
1. The API application failed to register its Prometheus metrics registry on startup.
2. The HTTP API service was stopped, deleted, or replaced with a misconfigured container.
3. The Prometheus scrape configuration job `orderflow-api` was commented out or corrupted.

## Diagnostic Queries & Commands
- **PromQL:** `absent(orderflow_http_requests_total)`
- **PromQL check:** `up{job="orderflow-api"}`
- **Manual scrape check:** `curl -s http://localhost:8080/metrics | grep orderflow_http_requests_total`

## Mitigation
1. Verify the API container is running: `docker compose ps api`.
2. Check if the `/metrics` endpoint is serving: `curl http://localhost:8080/metrics`.
3. If `/metrics` returns 404 or fails, verify `cmd/api/main.go` metrics registration and restart the API: `docker compose restart api`.
4. Inspect `deploy/prometheus/prometheus.yml` to verify the job definition is valid.
