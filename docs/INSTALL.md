# Install backstop OSS Without Cloning

The no-clone install path is designed for free users who want to run backstop with
Docker and configure AI tools through MCP.

## macOS And Linux

```bash
curl -fsSL https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.sh | sh
```

If the repo or branch is different:

```bash
BACKSTOP_REPO=pratyush2514/Backstop BACKSTOP_REF=main \
  curl -fsSL https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.sh | sh
```

The installer creates:

- `~/.backstop/docker-compose.yml`
- `~/.backstop/backstop.env`
- `~/.backstop/gateway-auth-tokens.json`
- `~/.backstop/bin/backstop-oss`

Add the command to your shell path if the installer prints a PATH line:

```bash
export PATH="$HOME/.backstop/bin:$PATH"
```

## Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/pratyush2514/Backstop/main/scripts/install.ps1 | iex
```

The installer creates:

- `%USERPROFILE%\.backstop\docker-compose.yml`
- `%USERPROFILE%\.backstop\backstop.env`
- `%USERPROFILE%\.backstop\gateway-auth-tokens.json`
- `%USERPROFILE%\.backstop\bin\backstop-oss.ps1`
- `%USERPROFILE%\.backstop\bin\backstop-oss.cmd`

It also adds the bin directory to the current user's PATH by default.

## Commands

```bash
backstop-oss doctor
backstop-oss up
backstop-oss seed
backstop-oss status
backstop-oss logs
backstop-oss mcp-config
backstop-oss down
```

The local stack exposes:

- Gateway: `http://localhost:8080`
- Sync metrics: `http://localhost:9091/metrics`
- PostgreSQL: `localhost:5433`
- MinIO API: `http://localhost:9000`
- MinIO console: `http://localhost:9001`

## MCP Usage

Print a config snippet:

```bash
backstop-oss mcp-config
```

Use `agent` mode for autonomous AI tools:

```text
BACKSTOP_MCP_MODE=agent
```

Use `operator` mode for human approval clients:

```text
BACKSTOP_MCP_MODE=operator
```

## Package Installs

Node packages are published once to npm and can be installed by npm-compatible
package managers:

```bash
npm install @backstop/client @backstop/mcp-server
pnpm add @backstop/client @backstop/mcp-server
yarn add @backstop/client @backstop/mcp-server
bun add @backstop/client @backstop/mcp-server
```

Python CLI:

```bash
pip install backstop
pipx install backstop
uv tool install backstop
```

## Important Release Requirement

The no-clone installer depends on published container images:

```text
ghcr.io/pratyush2514/backstop-gateway:<version>
ghcr.io/pratyush2514/backstop-sync:<version>
```

Before the first public release, replace the image namespace in
`~/.backstop/backstop.env` or publish images under the default namespace.

