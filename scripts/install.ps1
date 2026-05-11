param(
  [string]$Repo = $env:BACKSTOP_REPO,
  [string]$Ref = $env:BACKSTOP_REF,
  [string]$InstallDir = $env:BACKSTOP_INSTALL_DIR,
  [switch]$NoPath
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($Repo)) { $Repo = "pratyush2514/Backstop" }
if ([string]::IsNullOrWhiteSpace($Ref)) { $Ref = "main" }
if ([string]::IsNullOrWhiteSpace($InstallDir)) { $InstallDir = Join-Path $HOME ".backstop" }

$RawBase = if ($env:BACKSTOP_RAW_BASE) {
  $env:BACKSTOP_RAW_BASE.TrimEnd("/")
} else {
  "https://raw.githubusercontent.com/$Repo/$Ref"
}

function Require-Command($Name) {
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "backstop installer: missing required command: $Name"
  }
}

function Download-File($Url, $Path) {
  Invoke-WebRequest -Uri $Url -OutFile $Path -UseBasicParsing
}

Require-Command docker

$BinDir = Join-Path $InstallDir "bin"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

Download-File "$RawBase/deploy/docker-compose.oss.yml" (Join-Path $InstallDir "docker-compose.yml")
Download-File "$RawBase/deploy/seed.sql" (Join-Path $InstallDir "seed.sql")
Download-File "$RawBase/examples/gateway-auth-tokens.example.json" (Join-Path $InstallDir "gateway-auth-tokens.json")

$EnvPath = Join-Path $InstallDir "backstop.env"
if (-not (Test-Path -LiteralPath $EnvPath)) {
@"
BACKSTOP_VERSION=latest
BACKSTOP_GATEWAY_IMAGE=ghcr.io/pratyush2514/backstop-gateway:latest
BACKSTOP_SYNC_IMAGE=ghcr.io/pratyush2514/backstop-sync:latest

BACKSTOP_ENVIRONMENT=local
BACKSTOP_CLUSTER_ID=local
BACKSTOP_BUCKET=backstop-test
BACKSTOP_STORAGE=s3://backstop-test@http://minio:9000

POSTGRES_DB=testdb
POSTGRES_USER=postgres
POSTGRES_PASSWORD=password
POSTGRES_PORT=5433

MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
MINIO_PORT=9000
MINIO_CONSOLE_PORT=9001

BACKSTOP_GATEWAY_PORT=8080
BACKSTOP_SYNC_METRICS_PORT=9091
"@ | Set-Content -LiteralPath $EnvPath -Encoding UTF8
}

$PsShim = Join-Path $BinDir "backstop-oss.ps1"
@'
$ErrorActionPreference = "Stop"

$BackstopHome = if ($env:BACKSTOP_HOME) { $env:BACKSTOP_HOME } else { Join-Path $HOME ".backstop" }
$Compose = Join-Path $BackstopHome "docker-compose.yml"
$EnvFile = Join-Path $BackstopHome "backstop.env"
$Command = if ($args.Count -gt 0) { $args[0] } else { "help" }
$Rest = if ($args.Count -gt 1) { $args[1..($args.Count - 1)] } else { @() }

function Compose {
  docker compose --env-file $EnvFile -f $Compose -p backstop_oss @args
}

switch ($Command) {
  "up" { Compose up -d }
  "down" { Compose down }
  "reset" {
    Compose down -v
    Compose up -d
  }
  "status" { Compose ps }
  "logs" { Compose logs @Rest }
  "seed" {
    Compose exec -T postgres psql -U "postgres" -d "testdb" -v "ON_ERROR_STOP=1" -f "/seed/seed.sql"
  }
  "doctor" {
    docker version | Out-Null
    docker compose version | Out-Null
    Write-Output "backstop doctor: docker and compose are available"
  }
  "mcp-config" {
@"
{
  "mcpServers": {
    "backstop": {
      "command": "npx",
      "args": ["@backstop/mcp-server"],
      "env": {
        "BACKSTOP_URL": "http://localhost:8080",
        "BACKSTOP_TOKEN": "replace-agent-token",
        "BACKSTOP_AGENT_ID": "local-ai-agent",
        "BACKSTOP_MCP_MODE": "agent"
      }
    }
  }
}
"@
  }
  "where" { Write-Output $BackstopHome }
  default {
@"
backstop-oss commands:
  up          Start free OSS stack
  down        Stop stack
  reset       Stop stack, delete volumes, start fresh
  status      Show containers
  logs [svc]  Show logs
  seed        Load sample seed SQL into local Postgres
  doctor      Check Docker/Compose availability
  mcp-config  Print MCP config snippet
  where       Print install directory

Config files live in: $BackstopHome
"@
  }
}
'@ | Set-Content -LiteralPath $PsShim -Encoding UTF8

$CmdShim = Join-Path $BinDir "backstop-oss.cmd"
@"
@echo off
powershell -ExecutionPolicy Bypass -File "%USERPROFILE%\.backstop\bin\backstop-oss.ps1" %*
"@ | Set-Content -LiteralPath $CmdShim -Encoding ASCII

if (-not $NoPath) {
  $CurrentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($CurrentUserPath -split ';') -notcontains $BinDir) {
    [Environment]::SetEnvironmentVariable("Path", "$CurrentUserPath;$BinDir", "User")
  }
  if (($env:Path -split ';') -notcontains $BinDir) {
    $env:Path = "$env:Path;$BinDir"
  }
}

Write-Output ""
Write-Output "backstop OSS installed in $InstallDir"
Write-Output "Try:"
Write-Output "  backstop-oss doctor"
Write-Output "  backstop-oss up"
Write-Output "  backstop-oss mcp-config"

