# CI/CD and Deployment

## CI

GitHub Actions runs `.github/workflows/ci.yml` for pull requests to `main`, pushes to `main`, and manual dispatches. It checks Go formatting, vet, unit tests, API compilation, Compose configuration, and the API container build.

The workflow intentionally does not start the local E2E suite. E2E tests require an explicitly provisioned stack and are run only when their opt-in environment variable is supplied.

## Manual deployment

`.github/workflows/deploy.yml` deploys only when manually started from `main`. GitHub's `production` environment serializes deployments, and the workflow checks out the exact triggering commit on the VM before rebuilding the Compose stack and checking `/healthz`.

Configure these GitHub Actions secrets before triggering the workflow:

| Secret | Value |
| --- | --- |
| `DEPLOY_HOST` | VM DNS name or IP address |
| `DEPLOY_USER` | Least-privileged Linux account that can run Docker Compose |
| `DEPLOY_PATH` | Absolute path of the cloned OrderFlow repository on the VM |
| `DEPLOY_SSH_KEY` | Private SSH key for `DEPLOY_USER` |
| `DEPLOY_KNOWN_HOSTS` | Pinned SSH host-key entry from `ssh-keyscan` reviewed out of band |

Do not commit any of these values or place them in `.env.example`.

## VM preparation

Before the first deploy, install Docker Engine with Compose support, create the deployment account, clone the repository into `DEPLOY_PATH`, and give that account Docker access. Create production-only environment values on the VM in a local `.env` file. The workflow never transfers secrets from the repository.

The current Compose file binds API, PostgreSQL, Redis, Redpanda, and pgAdmin ports to `127.0.0.1`. This is correct for local development and VM-internal health checks. A future public deployment needs a separately configured HTTPS reverse proxy that exposes only the API; PostgreSQL, Redis, Redpanda, and pgAdmin must remain private.

## Docker connection map

| Client | PostgreSQL host | Port | Reason |
| --- | --- | --- | --- |
| OrderFlow API / pgAdmin Compose container | `postgres` | `5432` | Docker Compose service network |
| Windows host tools or desktop pgAdmin | `localhost` | `5433` | Host mapping avoids the existing local PostgreSQL on `5432` |
| Deployment VM host | `localhost` | `5433` | Same Compose host mapping; not publicly exposed |

Changing the Windows/VM host port does not require a change to `DATABASE_URL`: containers continue to connect through `postgres:5432`.
