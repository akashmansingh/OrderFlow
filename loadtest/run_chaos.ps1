param(
    [string]$RunId = "run-05-chaos",
    [string]$TargetUrl = "http://api:8080",
    [string]$DbUrl = "postgres://orderflow:local-dev-password@localhost:5433/orderflow?sslmode=disable",
    [string]$JwtSecret = "dev-jwt-secret-with-minimum-32-bytes-length-ok"
)

$ErrorActionPreference = "Stop"

$resultsDir = "results\$RunId"
if (-not (Test-Path $resultsDir)) {
    New-Item -ItemType Directory -Path $resultsDir -Force | Out-Null
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " STARTING CHAOS LOAD TEST BENCHMARK RUN: $RunId" -ForegroundColor Cyan
Write-Host " Output: $resultsDir" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Capture Environment
Write-Host "`n[1/6] Capturing benchmark environment..." -ForegroundColor Yellow
go run ./cmd/capture_env -run-id $RunId -out "$resultsDir\env.json"

# 2. Ensure test tokens exist
if (-not (Test-Path "loadtest\tokens.json") -or ((Get-Item "loadtest\tokens.json").Length -eq 0)) {
    Write-Host "`n[2/6] Seeding 1,000 test users..." -ForegroundColor Yellow
    go run ./cmd/seed -n 1000 -out "loadtest\tokens.json" -database-url $DbUrl -jwt-secret $JwtSecret
} else {
    Write-Host "`n[2/6] Test tokens present (loadtest\tokens.json)." -ForegroundColor Green
}

# 3. Launch k6 in background process
Write-Host "`n[3/6] Starting k6 chaos scenario in background..." -ForegroundColor Yellow
$loadtestHostPath = (Get-Item "loadtest").FullName
$resultsHostPath = (Get-Item $resultsDir).FullName
$k6OutputFile = "$resultsDir\k6_summary.json"

$k6Job = Start-Process -FilePath "docker" -ArgumentList @(
    "run", "--rm", "-i",
    "--network", "orderflow_default",
    "-v", "${loadtestHostPath}:/loadtest:ro",
    "-v", "${resultsHostPath}:/results",
    "grafana/k6", "run",
    "--summary-export", "/results/k6_summary.json",
    "-e", "TARGET_URL=$TargetUrl",
    "/loadtest/scenario5_chaos.js"
) -PassThru

# 4. Inject Chaos at controlled intervals
Write-Host "`n[4/6] Injecting chaos events mid-run..." -ForegroundColor Yellow

# T+6s: Restart worker container
Start-Sleep -Seconds 6
Write-Host "[Chaos T+6s] Restarting worker container mid-load..." -ForegroundColor Red
docker compose restart worker

# T+14s: Restart Redis container
Start-Sleep -Seconds 7
Write-Host "[Chaos T+14s] Restarting Redis container mid-load..." -ForegroundColor Red
docker compose restart redis

# T+20s: Inject duplicate Kafka messages
Start-Sleep -Seconds 6
Write-Host "[Chaos T+20s] Injecting duplicate Kafka messages into reservation-events..." -ForegroundColor Red
try {
    # Grab an existing message from Redpanda
    $rawMsg = docker exec orderflow-redpanda-1 rpk topic consume reservation-events -n 1 --offset start -f '%v\n'
    if ($rawMsg) {
        $msgLine = $rawMsg.Trim()
        # Produce duplicate message twice
        $msgLine | docker exec -i orderflow-redpanda-1 rpk topic produce reservation-events
        $msgLine | docker exec -i orderflow-redpanda-1 rpk topic produce reservation-events
        Write-Host "Injected duplicate Kafka message: $($msgLine.Substring(0, [Math]::Min(60, $msgLine.Length)))..." -ForegroundColor Green
    }
} catch {
    Write-Host "Warning injecting duplicate Kafka messages: $_" -ForegroundColor Yellow
}

Write-Host "Waiting for k6 process (PID $($k6Job.Id)) to complete..." -ForegroundColor Cyan
$k6Job.WaitForExit()

Write-Host "k6 execution finished with exit code $($k6Job.ExitCode)." -ForegroundColor Green

# Wait a brief moment for outbox relay and worker to settle
Write-Host "Allowing 5s for outbox relay and worker to settle remaining events..." -ForegroundColor Cyan
Start-Sleep -Seconds 5

# 5. Capture Metrics
Write-Host "`n[5/6] Capturing metrics snapshot..." -ForegroundColor Yellow
try {
    $metrics = Invoke-RestMethod -Uri "http://localhost:8080/metrics" -Method Get -TimeoutSec 5
    $metrics | Out-File -FilePath "$resultsDir\metrics.txt" -Encoding utf8
    Write-Host "Saved metrics snapshot to $resultsDir\metrics.txt" -ForegroundColor Green
} catch {
    Write-Host "Warning: Could not scrape metrics from localhost:8080: $_" -ForegroundColor Yellow
}

# 6. Verify Invariants
Write-Host "`n[6/6] Running database invariant checker (cmd/verify)..." -ForegroundColor Yellow
$verifyOutput = go run ./cmd/verify -database-url $DbUrl 2>&1
$verifyOutput | Out-File -FilePath "$resultsDir\verify.txt" -Encoding utf8
$verifyOutput | Write-Host

Write-Host "`nChaos run $RunId completed successfully." -ForegroundColor Green
Write-Host "Results saved to $resultsDir" -ForegroundColor Green
