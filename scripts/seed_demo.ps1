# Reset demo events and flush Redis cache
Write-Host "Resetting demo events and flushing Redis cache..."
Get-Content scripts/seed_demo.sql | docker exec -i orderflow-postgres-1 psql -U orderflow -d orderflow
docker exec -i orderflow-redis-1 redis-cli FLUSHALL | Out-Null
Write-Host "Showcase events seeded successfully! Refresh your browser."
