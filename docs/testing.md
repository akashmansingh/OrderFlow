# Testing

## Standard checks

The CI workflow runs these checks for pull requests and commits to `main`:

```powershell
gofmt -l .
go vet ./...
go test ./...
go build ./cmd/api
docker compose config --quiet
docker build --tag orderflow-api:ci .
```

## Opt-in end-to-end tests

`tests/e2e/api_test.go` exercises the running HTTP API. It deliberately skips unless `ORDERFLOW_E2E_BASE_URL` is set, so `go test ./...` remains safe in CI without a live Docker stack.

After starting the stack with `docker compose up -d --wait`, run the live suite explicitly:

```powershell
$env:ORDERFLOW_E2E_BASE_URL = "http://127.0.0.1:8080"
go test ./tests/e2e -v
Remove-Item Env:ORDERFLOW_E2E_BASE_URL
```

The suite creates isolated events and UUIDs. It checks API health, ticket reservation creation, idempotent retries, and capacity-based oversell rejection. Do not run it against a shared or production environment.
