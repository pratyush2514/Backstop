param(
  [string]$ComposeFile = "deploy\docker-compose.yml",
  [string]$Project = "backstop_e2e",
  [int]$TimeoutSeconds = 180,
  [switch]$Fresh
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

if ($Fresh) {
  docker compose -f $ComposeFile -p $Project down -v --remove-orphans
}

docker compose -f $ComposeFile -p $Project up -d --remove-orphans

$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
do {
  $ps = docker compose -f $ComposeFile -p $Project ps --format json | ConvertFrom-Json
  $bad = @($ps | Where-Object { $_.State -eq "exited" -or $_.State -eq "dead" })
  if ($bad.Count -gt 0) {
    docker compose -f $ComposeFile -p $Project logs --tail=160
    throw "One or more services exited during startup: $($bad.Name -join ', ')"
  }
  $unhealthy = @($ps | Where-Object { $_.Health -and $_.Health -ne "healthy" })
  if ($unhealthy.Count -eq 0 -and $ps.Count -ge 4) {
    docker compose -f $ComposeFile -p $Project ps
    exit 0
  }
  Start-Sleep -Seconds 3
} while ((Get-Date) -lt $deadline)

docker compose -f $ComposeFile -p $Project ps
docker compose -f $ComposeFile -p $Project logs --tail=200
throw "Timed out waiting for healthy services"
