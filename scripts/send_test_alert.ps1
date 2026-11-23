param(
    [string]$AlertmanagerUrl = "http://localhost:9093"
)

$ErrorActionPreference = "Stop"
$now = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")

$body = @"
[
  {
    "labels": {
      "alertname": "TestManualAlert",
      "severity": "warning",
      "service": "orderflow-api",
      "instance": "manual-test"
    },
    "annotations": {
      "summary": "Manual test alert from OrderFlow verification",
      "description": "Verifying Alertmanager webhook notifications and routing.",
      "runbook_url": "docs/runbooks/target-down.md"
    },
    "startsAt": "$now"
  }
]
"@

Write-Host "Sending test alert to Alertmanager at $AlertmanagerUrl..." -ForegroundColor Cyan
$resp = Invoke-RestMethod -Uri "$AlertmanagerUrl/api/v2/alerts" -Method Post -Body $body -ContentType "application/json"
Write-Host "Test alert successfully submitted to Alertmanager." -ForegroundColor Green
