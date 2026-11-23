param(
    [string]$TargetUrl = "http://localhost:8080"
)

Write-Host "Starting continuous background demo traffic against $TargetUrl (Ctrl+C to stop)..." -ForegroundColor Cyan

while ($true) {
    try {
        $null = Invoke-RestMethod -Uri "$TargetUrl/healthz" -Method Get -TimeoutSec 2
    } catch {}
    Start-Sleep -Milliseconds 500
}
