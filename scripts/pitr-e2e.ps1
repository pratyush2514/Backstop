param(
  [string]$ComposeFile = "deploy\docker-compose.yml",
  [string]$Project = "backstop_pitr_e2e",
  [int]$TimeoutSeconds = 360,
  [int]$GatewayPort = 18088,
  [int]$PostgresPort = 15441,
  [int]$MinioPort = 19010,
  [int]$MinioConsolePort = 19011,
  [int]$SyncPort = 19099
)

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

node scripts/pitr-e2e.mjs `
  --compose-file $ComposeFile `
  --project $Project `
  --timeout-seconds $TimeoutSeconds `
  --gateway-port $GatewayPort `
  --postgres-port $PostgresPort `
  --minio-port $MinioPort `
  --minio-console-port $MinioConsolePort `
  --sync-port $SyncPort
