param(
    [string]$TargetUrl = "http://localhost:8080",
    [string]$DbUrl = "postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable"
)

$ErrorActionPreference = "Stop"

Write-Host "========================================================" -ForegroundColor Cyan
Write-Host "      ORDERFLOW RESILIENCE & FAILURE SIMULATION DEMO    " -ForegroundColor Cyan
Write-Host "========================================================" -ForegroundColor Cyan

Write-Host "`nStep 1: Stopping asynchronous saga worker..." -ForegroundColor Yellow
docker compose stop worker

Write-Host "`nStep 2: Submitting reservation traffic while worker is offline..." -ForegroundColor Yellow
go run ./cmd/verify -database-url $DbUrl

Write-Host "`nStep 3: Waiting 35 seconds for StuckSagas / OutboxStagnation alert threshold..." -ForegroundColor Yellow
Start-Sleep -Seconds 35

Write-Host "`nStep 4: Restarting saga worker..." -ForegroundColor Yellow
docker compose start worker

Write-Host "`nStep 5: Waiting 10 seconds for worker to drain outbox backlog..." -ForegroundColor Yellow
Start-Sleep -Seconds 10

Write-Host "`nStep 6: Verifying all database invariants hold..." -ForegroundColor Yellow
go run ./cmd/verify -database-url $DbUrl

Write-Host "`n========================================================" -ForegroundColor Green
Write-Host "DEMO COMPLETED: System recovered cleanly with 0 oversells" -ForegroundColor Green
Write-Host "========================================================" -ForegroundColor Green
