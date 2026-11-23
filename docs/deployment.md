# OrderFlow Production Deployment Guide

## Architecture & Hosting Specifications

OrderFlow is designed for zero-downtime, reproducible deployment on a single cloud VM (e.g., Hetzner CPX21, AWS EC2 t4g.medium, or Oracle Cloud Always Free 4-core Ampere):
- **CPU:** Minimum 2 vCPU
- **RAM:** Minimum 4 GB RAM
- **Storage:** 40 GB NVMe / SSD
- **Operating System:** Ubuntu 22.04 LTS or 24.04 LTS

```text
Internet
   │ (HTTPS: 443 / HTTP: 80)
   ▼
[ Caddy Reverse Proxy (Automatic TLS) ]
   ├── api.orderflow.example     ──► [ orderflow-api :8080 ] (Blocks /metrics)
   └── grafana.orderflow.example ──► [ grafana :3000 ] (Anonymous Viewer / Admin auth)
                                              │
Internal Docker Bridge Network                │ (PromQL)
─────────────────────────────────────────────┼────────────────────────────
                                             ▼
                                     [ prometheus :9090 ] (15d retention)
                                             │
                       ┌─────────────────────┴─────────────────────┐
                       ▼                                           ▼
             [ alertmanager :9093 ]                       [ Infrastructure ]
                       │                                   ├── postgres (5432)
                       ▼                                   ├── redis (6379)
             [ Webhook / Discord / Slack ]                 ├── redpanda (9092)
                                                           └── exporters (node, cadvisor)
```

---

## 1. DNS Configuration

Create two DNS `A` records pointing to your cloud VM public IPv4 address:
- `api.<your-domain>.com` $\rightarrow$ `<VM_PUBLIC_IP>`
- `grafana.<your-domain>.com` $\rightarrow$ `<VM_PUBLIC_IP>`

*(Optional: Use free wildcard DNS services such as DuckDNS or nip.io for staging).*

---

## 2. Firewall Configuration (UFW)

Restrict incoming traffic so that **only** SSH (22), HTTP (80), and HTTPS (443) are publicly reachable. All backend services (PostgreSQL, Redpanda, Redis, Prometheus, Alertmanager) bind exclusively to the internal Docker network.

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP (Let's Encrypt ACME challenge)
sudo ufw allow 443/tcp   # HTTPS (TLS traffic)
sudo ufw enable
```

---

## 3. First-Time Production Deployment

1. **Clone repository on the VM:**
   ```bash
   git clone https://github.com/theRiddler9/OrderFlow.git /opt/orderflow
   cd /opt/orderflow
   ```

2. **Configure production environment:**
   Create `.env` inside `/opt/orderflow` (never committed to git):
   ```ini
   DOMAIN=yourdomain.com
   ACME_EMAIL=admin@yourdomain.com
   POSTGRES_USER=orderflow
   POSTGRES_PASSWORD=generate-a-strong-random-password-here
   POSTGRES_DB=orderflow
   JWT_SECRET=generate-a-cryptographically-secure-32-byte-secret
   GRAFANA_ADMIN_USER=admin
   GRAFANA_ADMIN_PASSWORD=generate-a-strong-grafana-password
   GRAFANA_ANONYMOUS_VIEWER=true
   ALERTMANAGER_WEBHOOK_URL=https://discord.com/api/webhooks/...
   TAG=latest
   ```

3. **Deploy production compose stack:**
   ```bash
   docker compose -f deploy/compose.prod.yaml pull
   docker compose -f deploy/compose.prod.yaml up -d
   ```

4. **Verify deployment health:**
   ```bash
   curl -I https://api.yourdomain.com/healthz
   # Expected: HTTP/2 200 OK
   
   curl -I https://api.yourdomain.com/metrics
   # Expected: HTTP/2 403 Forbidden (Blocked by Caddy)
   ```

---

## 4. Operator SSH Tunnels for Internal Observability

Prometheus (`:9090`) and Alertmanager (`:9093`) are not exposed publicly. Operators access them securely via an encrypted SSH tunnel:

```bash
ssh -L 9090:localhost:9090 -L 9093:localhost:9093 user@<VM_PUBLIC_IP>
```
Now browse to:
- Prometheus UI: `http://localhost:9090`
- Alertmanager UI: `http://localhost:9093`

---

## 5. Automated Rollback & CI/CD

Deployments are automated via `.github/workflows/deploy.yml`:
1. On merge to `main`, GitHub Actions runs the full test suite, builds Docker images, and tags them with the Git commit SHA.
2. Pushes images to GitHub Container Registry (GHCR).
3. Connects via SSH to the VM and issues `docker compose -f deploy/compose.prod.yaml up -d`.
4. Executes an automated smoke test against `https://api.<domain>/healthz`.
5. **Rollback Trigger:** If the smoke test fails, the workflow immediately executes automated rollback to restore the previous container image tag.

---

## 6. Disaster Recovery & Backups

### Automated Logical Backup:
```bash
./scripts/backup.sh
# Creates gzip compressed backup in backups/orderflow_YYYYMMDD_HHMMSS.sql.gz
```

### Verified Restore Procedure:
```bash
./scripts/restore.sh backups/orderflow_20261123_120000.sql.gz
# Streams database restore and immediately executes invariant validation audit (cmd/verify)
```
