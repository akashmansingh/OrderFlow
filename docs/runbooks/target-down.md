# Runbook: TargetDown

## Symptom
Prometheus reports `up == 0` for one or more monitored services or exporters for > 1 minute.

## Likely Causes
1. The container or process crashed (OOM killed, unhandled panic, or segmentation fault).
2. Network partition or Docker bridge network disruption between Prometheus and the target.
3. Target is overloaded and dropping HTTP scraping connections.

## Diagnostic Queries & Commands
- **PromQL:** `up == 0`
- **Docker logs:** `docker compose logs --tail=100 <service_name>`
- **Container status:** `docker compose ps`
- **Container resource stats:** `docker stats --no-stream`

## Mitigation
1. Inspect container logs: `docker compose logs <service_name>`. Look for OOM kills (`exit code 137`) or fatal errors.
2. If stopped or unhealthy, restart the service: `docker compose restart <service_name>`.
3. If container repeatedly crashes on startup, inspect environment variables and database/broker connectivity.
4. Verify scrape network connectivity: `docker compose exec prometheus wget -qO- http://<target>:<port>/metrics`.
