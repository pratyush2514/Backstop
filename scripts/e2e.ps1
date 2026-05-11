param(
  [string]$ComposeFile = "deploy\docker-compose.yml",
  [string]$Project = "backstop_oss_e2e",
  [string]$Token = "dev-token",
  [int]$TimeoutSeconds = 180,
  [int]$GatewayPort = 8080,
  [int]$PostgresPort = 5433,
  [int]$MinioPort = 9000,
  [int]$MinioConsolePort = 9001,
  [int]$SyncPort = 9091
)

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

node scripts/e2e.mjs `
  --compose-file $ComposeFile `
  --project $Project `
  --token $Token `
  --timeout-seconds $TimeoutSeconds `
  --gateway-port $GatewayPort `
  --postgres-port $PostgresPort `
  --minio-port $MinioPort `
  --minio-console-port $MinioConsolePort `
  --sync-port $SyncPort
