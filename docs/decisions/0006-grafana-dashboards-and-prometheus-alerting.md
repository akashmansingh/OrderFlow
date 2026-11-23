# ADR 0006: Grafana Dashboards-as-Code, Prometheus Alerting & Production Deployment Topology

## Status

Accepted

## Context

Operating a mission-critical ticket reservation platform under extreme contention requires comprehensive observability across both application and infrastructure layers, actionable alerting with low alert fatigue, and a secure deployment perimeter:
1. Application metrics alone do not reveal PostgreSQL connection pool saturation, Redis memory exhaustion, Redpanda partition lag, or container CPU throttling.
2. Un-routed alerts only visible in Prometheus UI result in unobserved outages. Alerts must route to webhook channels (Discord/Slack) with clear severity, flapping prevention (`for:` duration), and direct links to diagnostic runbooks.
3. Dashboard queries calculating expensive percentiles (`p95`, `p99`) across millions of requests cause severe Prometheus query CPU spikes; recording rules must pre-compute these series.
4. Exposing Prometheus, Alertmanager, or `/metrics` to the public internet presents a severe reconnaissance and denial-of-service vector.

## Decisions

1. **Alertmanager Integration & Webhook Routing:**
   - Add Alertmanager (`prom/alertmanager:v0.27.0`) with routing rules, grouping by `['alertname', 'service', 'job']`, and inhibition rules suppressing warning alerts when critical alerts are already firing on the same service.
   - Configure webhook receiver (`ALERTMANAGER_WEBHOOK_URL`) for notification delivery.

2. **Infrastructure Observability via Exporters:**
   - Add `postgres-exporter` (connections, transaction rates, locks), `redis-exporter` (memory, ops/sec, evictions), Redpanda `/public_metrics` (broker latency, partition lag), `node-exporter` (VM CPU, memory, disk), and `cadvisor` (container resource usage).
   - Impose explicit Docker resource constraints on every container.

3. **Multi-Window Burn-Rate SLO Alerts & Runbooks:**
   - Define multi-window burn-rate alerts based on Google SRE principles for a 99.9% availability SLO (14.4x burn rate over 1h/5m for paging; 6x burn rate over 6h/30m for ticketing) and a 99% < 500ms latency SLO.
   - Every alert includes `severity`, `for:`, `summary`, `description`, and a `runbook_url` pointing to a markdown runbook in `docs/runbooks/<alert>.md` with symptoms, likely causes, PromQL diagnostics, and step-by-step mitigation.

4. **Recording Rules:**
   - Implement pre-aggregated recording rules in `deploy/prometheus/recording_rules.yml` for `p50`, `p95`, `p99` latency, `rate5m` request volume, 5xx error ratios, and saga duration percentiles to ensure sub-second dashboard rendering.

5. **Dashboards-as-Code Architecture:**
   - Maintain three separate dashboards with stable UIDs:
     - `orderflow-overview`: Executive traffic, 5xx ratio, p50/p95/p99 latency, saga throughput, SLO error budget gauge, stuck sagas stat, deploy annotations.
     - `orderflow-pipeline`: Outbox backlog, oldest row age, consumer lag per partition, worker retries, DLQ event count by reason.
     - `orderflow-infra`: PostgreSQL connection pools, transaction rates, Redis memory & fail-open events, Redpanda broker latency, container CPU/RAM.

6. **Network & Perimeter Security:**
   - Public ingress is strictly restricted to Caddy reverse proxy (`:80`, `:443`).
   - Caddy explicitly intercepts and blocks `/metrics` with `HTTP 403 Forbidden`.
   - PostgreSQL, Redis, Redpanda, Prometheus, and Alertmanager are isolated on an internal bridge network with zero public port mappings in production. Operators access Prometheus/Alertmanager via encrypted SSH tunnels.
   - Grafana admin credentials originate from environment secrets; user registration is disabled; anonymous access is limited to Viewer role.

7. **Disaster Recovery & CI/CD Deployment:**
   - Provide automated `scripts/backup.sh` (pg_dump with gzip) and `scripts/restore.sh` with automated post-restore invariant checking (`cmd/verify`).
   - GitHub Actions automated deployment on push to `main` with smoke-test validation and automatic rollback.

## Consequences

- End-to-end operational visibility with low query overhead and fast dashboard loading.
- High-fidelity alerting with clear runbooks and zero public attack surface for internal monitoring components.
- Reproducible, automated deployment topology with guaranteed rollback on failure.
