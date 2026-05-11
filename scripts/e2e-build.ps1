param(
  [string]$ComposeFile = "deploy\docker-compose.yml",
  [string]$Project = "backstop_e2e",
  [switch]$CleanImages
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $root

if ($CleanImages) {
  $images = @(
    "$Project-postgres:latest",
    "$Project-backstop-gateway:latest",
    "$Project-backstop-sync:latest"
  )
  foreach ($image in $images) {
    docker image inspect $image *> $null
    if ($LASTEXITCODE -eq 0) {
      docker rmi $image
    }
  }
}

docker compose -f $ComposeFile -p $Project config --quiet
docker compose -f $ComposeFile -p $Project build --pull --progress plain
