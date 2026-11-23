param(
    [string]$Scenario = "scenario1_hot_sku.js",
    [string]$RunId = "",
    [string]$TargetUrl = "http://api:8080",
    [string]$DbUrl = "postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable",
    [string]$JwtSecret = "dev-jwt-secret-with-minimum-32-bytes-length-ok"
)

$ErrorActionPreference = "Stop"

if (-not $RunId) {
    $RunId = "run-" + (Get-Date -Format "yyyyMMdd-HHmmss")
}

$resultsDir = "results\$RunId"
if (-not (Test-Path $resultsDir)) {
    New-Item -ItemType Directory -Path $resultsDir -Force | Out-Null
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " STARTING LOAD TEST BENCHMARK RUN: $RunId" -ForegroundColor Cyan
Write-Host " Scenario: $Scenario" -ForegroundColor Cyan
Write-Host " Output:   $resultsDir" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Capture Environment
Write-Host "`n[1/5] Capturing benchmark environment..." -ForegroundColor Yellow
go run ./cmd/capture_env -run-id $RunId -out "$resultsDir\env.json"

# 2. Ensure test users are seeded
if (-not (Test-Path "loadtest\tokens.json") -or ((Get-Item "loadtest\tokens.json").Length -eq 0)) {
    Write-Host "`n[2/5] Seeding 1,000 test users..." -ForegroundColor Yellow
    go run ./cmd/seed -n 1000 -out "loadtest\tokens.json" -database-url $DbUrl -jwt-secret $JwtSecret
} else {
    Write-Host "`n[2/5] Test tokens present (loadtest\tokens.json)." -ForegroundColor Green
}

# 3. Execute k6 via Docker container connected to orderflow network
Write-Host "`n[3/5] Executing k6 scenario ($Scenario)..." -ForegroundColor Yellow
$loadtestHostPath = (Get-Item "loadtest").FullName
$resultsHostPath = (Get-Item $resultsDir).FullName
$k6OutputFile = "$resultsDir\k6_summary.json"

docker run --rm -i `
    --network orderflow_default `
    -v "${loadtestHostPath}:/loadtest:ro" `
    -v "${resultsHostPath}:/results" `
    grafana/k6 run `
    --summary-export "/results/k6_summary.json" `
    -e TARGET_URL=$TargetUrl `
    "/loadtest/$Scenario"

if (Test-Path $k6OutputFile) {
    Write-Host "Saved raw k6 results to $k6OutputFile" -ForegroundColor Green
}

# 4. Capture Prometheus Metrics Snapshot
Write-Host "`n[4/5] Capturing metrics snapshot..." -ForegroundColor Yellow
try {
    $metrics = Invoke-RestMethod -Uri "http://localhost:8080/metrics" -Method Get -TimeoutSec 5
    $metrics | Out-File -FilePath "$resultsDir\metrics.txt" -Encoding utf8
    Write-Host "Saved metrics snapshot to $resultsDir\metrics.txt" -ForegroundColor Green
} catch {
    Write-Host "Warning: Could not scrape metrics from localhost:8080: $_" -ForegroundColor Yellow
}

# 5. Run Database Invariant Checker
Write-Host "`n[5/5] Running database invariant checker (cmd/verify)..." -ForegroundColor Yellow
$maxAttempts = 40
$verifyOutput = ""
for ($i = 1; $i -le $maxAttempts; $i++) {
    $verifyOutput = go run ./cmd/verify -database-url $DbUrl 2>&1
    if ($LASTEXITCODE -eq 0) {
        break
    }
    Write-Host "Waiting for outbox/inbox to drain (attempt $i/$maxAttempts)..." -ForegroundColor Cyan
    Start-Sleep -Seconds 2
}
$verifyOutput | Out-File -FilePath "$resultsDir\verify.txt" -Encoding utf8
$verifyOutput | Write-Host

Write-Host "`nBenchmark run $RunId completed successfully." -ForegroundColor Green
Write-Host "Results saved to $resultsDir" -ForegroundColor Green
