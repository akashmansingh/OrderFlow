# Implementation Plan: Stage 8 - Deployment-Ready Observability & Production Hardening

## Overview
This plan implements production-ready observability (Alertmanager, Prometheus recording/alerting rules with multi-window burn rates, Promtool tests, Postgres/Redis/Redpanda/Node/cAdvisor exporters, three Grafana Dashboards-as-Code) and production deployment architecture (Caddy reverse proxy with automatic HTTPS and `/metrics` blocking, production Docker Compose, CI/CD deployment workflow, backup/restore scripts, demo traffic/failure automation, and runbooks).

---

## Phase 1: Metric Cardinality Audit & Guardrail Tests
- **Goal:** Guarantee all metric labels maintain bounded cardinality (route templates only, strictly preventing UUIDs, user IDs, or reservation IDs).
- **Files:**
  - `internal/metrics/metrics.go` (audit labels)
  - `internal/metrics/cardinality_test.go` (test preventing forbidden labels: `user_id`, `reservation_id`, `customer_id`, `id`)

## Phase 2: Exporters, Alertmanager & Prometheus Topology
- **Goal:** Instrument the entire infrastructure stack and configure alert routing.
- **Files:**
  - `compose.yaml`:
    - Add `alertmanager`: `prom/alertmanager:v0.27.0` (0.2 CPU, 64MB RAM)
    - Add `prometheus`: `prom/prometheus:v2.54.1` (0.5 CPU, 256MB RAM)
    - Add `postgres-exporter`: `prometheuscommunity/postgres-exporter:v0.15.0` (0.2 CPU, 64MB RAM)
    - Add `redis-exporter`: `oliver006/redis_exporter:v1.62.0` (0.2 CPU, 64MB RAM)
    - Add `node-exporter`: `prom/node-exporter:v1.8.2` (0.2 CPU, 64MB RAM)
    - Add `cadvisor`: `gcr.io/cadvisor/cadvisor:v0.49.1` (0.3 CPU, 128MB RAM)
    - Add `grafana`: `grafana/grafana:11.2.0` (0.5 CPU, 256MB RAM)
  - `deploy/alertmanager/alertmanager.yml`:
    - Webhook routing via `ALERTMANAGER_WEBHOOK_URL`
    - Grouping rules (`group_by: ['alertname', 'cluster', 'service']`)
    - Inhibition rules (inhibit warning if critical is active)
  - `deploy/prometheus/prometheus.yml`:
    - Scrape configs: `orderflow-api`, `postgres-exporter`, `redis-exporter`, `redpanda` (`:9644/public_metrics`), `node-exporter`, `cadvisor`, `alertmanager`.
  - `scripts/send_test_alert.sh` / `scripts/send_test_alert.ps1`:
    - Script to trigger a test alert in Alertmanager via API v2.

## Phase 3: Prometheus Recording Rules, Multi-Window SLO Alerts & Runbooks
- **Goal:** Compute expensive metrics ahead of time, define resilient SLO burn-rate alerts, and document actionable runbooks.
- **Files:**
  - `deploy/prometheus/recording_rules.yml`:
    - Latency quantiles: `p50`, `p95`, `p99`
    - Throughput: `rate5m`
    - Error ratios: `job:orderflow_http_errors:ratio5m`
    - Saga durations: `job:orderflow_worker_saga_duration_seconds:p95`
  - `deploy/prometheus/alerts.yml`:
    - `TargetDown` (`up == 0` for 1m)
    - `AbsentMetric` (`absent(orderflow_http_requests_total)` for 2m)
    - `SLOAvailabilityBurnRatePage` (1h burn rate > 14.4 for 2m)
    - `SLOAvailabilityBurnRateTicket` (6h burn rate > 6 for 15m)
    - `SLOLatencyBurnRate` (p95 > 500ms for 5m)
    - `StuckSagas` (reservations pending > 30s)
    - `OutboxStagnation` (oldest row age > 30s)
    - `ConsumerLagSpike` (lag > 50)
    - `DeadLetterQueueActive` (DLQ count > 0)
    - `RedisFailOpenActive` (fail open > 0)
    - `PostgresConnectionsHigh` (> 80%)
    - `RedisMemoryHigh` (> 85%)
    - Annotations on every alert: `severity`, `for`, `summary`, `description`, `runbook_url`.
  - `docs/runbooks/*.md`:
    - `target-down.md`, `metric-absent.md`, `slo-availability-burn-rate.md`, `slo-latency-burn-rate.md`, `stuck-sagas.md`, `outbox-stagnation.md`, `consumer-lag-spike.md`, `dead-letter-queue-active.md`, `redis-fail-open-active.md`, `postgres-connections-high.md`, `redis-memory-high.md`.

## Phase 4: Promtool Rule Testing & Dashboard Linting
- **Goal:** Verify alerting logic using synthetic timeseries tests with `promtool`.
- **Files:**
  - `deploy/prometheus/test_alerts.yml`:
    - Unit tests asserting alert firing and resolution for `TargetDown`, `AbsentMetric`, `StuckSagas`, `SLOAvailabilityBurnRatePage`, `DeadLetterQueueActive`.
  - CI verification commands:
    - `promtool check rules deploy/prometheus/recording_rules.yml deploy/prometheus/alerts.yml`
    - `promtool check config deploy/prometheus/prometheus.yml`
    - `promtool test rules deploy/prometheus/test_alerts.yml`

## Phase 5: Grafana Dashboards-as-Code
- **Goal:** Provision three structured dashboards with stable UIDs, variables, and runbook links.
- **Files:**
  - `deploy/grafana/provisioning/datasources/prometheus.yml`
  - `deploy/grafana/provisioning/dashboards/dashboards.yml`
  - `deploy/grafana/dashboards/overview.json` (`UID: orderflow-overview`):
    - Real-time RPS, 5xx ratio, p50/p95/p99 from recording rules, saga throughput, SLO error-budget gauge, stuck sagas panel, git SHA deploy annotation.
  - `deploy/grafana/dashboards/pipeline.json` (`UID: orderflow-pipeline`):
    - Outbox backlog & age, Redpanda partition lag, worker saga duration, retries, DLQ event counts.
  - `deploy/grafana/dashboards/infra.json` (`UID: orderflow-infra`):
    - Postgres connections/locks, Redis memory/ops/fail-open, Redpanda throughput, container CPU & memory.

## Phase 6: Security, Reverse Proxy & Deployment Configurations
- **Goal:** Restrict internal endpoints, configure Caddy with HTTPS, and prepare production compose and CI/CD.
- **Files:**
  - `deploy/caddy/Caddyfile`:
    - Public domain routing: `api.{$DOMAIN}` and `grafana.{$DOMAIN}`
    - Automatic blocking of `/metrics` (returns 403 Forbidden)
    - Zero host port exposure for Prometheus, Alertmanager, Postgres, Redis, Kafka
  - `tests/e2e/security_test.go`:
    - Automated test proving `/metrics` blocked on public proxy
  - `deploy/compose.prod.yaml`:
    - Named volumes: `postgres_data`, `redis_data`, `redpanda_data`, `prometheus_data`, `grafana_data`, `caddy_data`, `caddy_config`
    - Restart policies (`restart: unless-stopped`)
    - Prometheus retention (`--storage.tsdb.retention.time=15d`)
    - Healthchecks on all dependencies
  - `.github/workflows/deploy.yml`:
    - Build & push to GHCR tagged with git SHA
    - SSH deployment to cloud VM with automatic rollback on smoke-test failure

## Phase 7: Backup/Restore & Demo Automation
- **Goal:** Reliable database recovery and live demo traffic generation.
- **Files:**
  - `scripts/backup.sh` & `scripts/restore.sh`:
    - `pg_dump` with gzip compression, timestamping, validation test with `cmd/verify`
  - `scripts/demo_traffic.sh` (or `make demo-traffic`):
    - Steady low-rate traffic generator to keep public Grafana waveforms populated
  - `scripts/demo_failure.sh` (or `make demo-failure`):
    - Worker kill simulation, alerting verification, worker restart, and recovery confirmation

## Phase 8: Documentation & ADR Updates
- **Goal:** Comprehensive runbooks, deployment guides, and decision records.
- **Files:**
  - `docs/DEPLOYMENT.md`: VM specs, DNS, firewall, initial deployment runbook, SSH tunnels, rollback.
  - `docs/decisions/0006-grafana-dashboards-and-prometheus-alerting.md`: Architecture Decision Record.
  - `README.md`: "Live demo" section with link, screenshot placeholders, and failure demo walkthrough.
