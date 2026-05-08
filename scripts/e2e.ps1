param(
  [string]$ComposeFile = "deploy\docker-compose.yml",
  [string]$Project = "backstop_oss_e2e",
  [string]$Token = "dev-token",
  [int]$TimeoutSeconds = 180
)

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

node scripts/e2e.mjs `
  --compose-file $ComposeFile `
  --project $Project `
  --token $Token `
  --timeout-seconds $TimeoutSeconds

