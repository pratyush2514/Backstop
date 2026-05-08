# Publishing backstop OSS

This repo can publish:

- `@backstop/client` to npm
- `@backstop/mcp-server` to npm
- `backstop` to PyPI
- `backstop-gateway` and `backstop-sync` to GHCR

## Before First Publish

Confirm ownership first:

1. npm scope: `@backstop` or your chosen replacement scope
2. PyPI project name: `backstop` or your chosen replacement name
3. GHCR namespace: `ghcr.io/pratyush2514/...`

If a public name is unavailable, rename before release. Do not publish docs
that promise a package name you do not own.

## Required GitHub Setup

### npm

Create an npm automation token and add:

```text
NPM_TOKEN
```

The workflow publishes with:

```text
npm publish --access public --provenance
```

### PyPI

Prefer Trusted Publishing:

1. Create the PyPI project.
2. Add this GitHub repository as a trusted publisher in PyPI.
3. Use the `pypi` GitHub environment for the publish job.

No long-lived PyPI token is required if Trusted Publishing is configured.

### GHCR

GHCR publishing uses the repository `GITHUB_TOKEN`. The workflow pushes to:

```text
ghcr.io/pratyush2514/backstop-gateway:<tag>
ghcr.io/pratyush2514/backstop-sync:<tag>
ghcr.io/pratyush2514/backstop-cli:<tag>
```

`latest` is also pushed for tag releases.

## Release Flow

1. Update versions:
   Node packages in [packages/node-sdk/package.json](/C:/Users/Pratyush/Downloads/dbguard/packages/node-sdk/package.json) and [packages/mcp-server/package.json](/C:/Users/Pratyush/Downloads/dbguard/packages/mcp-server/package.json)
   Python package in [sdk/python/pyproject.toml](/C:/Users/Pratyush/Downloads/dbguard/sdk/python/pyproject.toml)

2. Run local checks:

```bash
npm ci
npm test
npm run pack:check
python -m pytest sdk/python/tests -q
```

3. Create and push a tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

4. GitHub Actions runs:
   - `release-packages`
   - `docker-images`

## What The Workflows Do

`release-packages.yml`
- installs dependencies
- runs Node tests
- publishes `@backstop/client`
- publishes `@backstop/mcp-server`
- builds and checks the Python distribution
- publishes Python package to PyPI

`docker-images.yml`
- logs in to GHCR
- builds images for gateway, sync, and CLI
- pushes version tags
- pushes `latest` for tag releases

## Free User Install Targets

Once published, the documented free-user install paths are:

```bash
npm install @backstop/client
pnpm add @backstop/client
yarn add @backstop/client
bun add @backstop/client

npm install @backstop/mcp-server
pnpm add @backstop/mcp-server
yarn add @backstop/mcp-server
bun add @backstop/mcp-server

pip install backstop
pipx install backstop
uv tool install backstop
```

Containers:

```bash
docker pull ghcr.io/pratyush2514/backstop-gateway:<version>
docker pull ghcr.io/pratyush2514/backstop-sync:<version>
```

## Current Boundary

The repo is now set up for publishing automation, but the final publish still
depends on:

- your actual npm scope ownership
- your actual PyPI project ownership
- your GitHub owner/org namespace
- the `NPM_TOKEN` secret
- PyPI trusted publisher configuration

