# Contributing To backstop

Thanks for helping make AI-agent database access safer.

## Development Setup

Requirements:

- Go 1.22+
- Python 3.12+
- Node.js 18+
- Docker Desktop or Docker Engine

Run the main checks:

```powershell
npm install
npm test
cd gateway; $env:CGO_ENABLED='0'; go test ./... -count=1
cd ..\sync; $env:CGO_ENABLED='0'; go test ./... -count=1
cd ..\sdk\python; python -m pytest tests -q
```

Run the local E2E:

```bash
npm run e2e
```

## Pull Request Expectations

- Keep changes scoped.
- Add tests for safety behavior, recovery behavior, and API contract changes.
- Do not add paid or proprietary runtime dependencies to the OSS core.
- Do not weaken fail-closed behavior without an explicit policy flag and tests.
- Update docs when changing setup, APIs, or safety guarantees.

## Safety Philosophy

backstop should be honest about what it can and cannot protect:

- It prevents dangerous SQL only when traffic is routed through the gateway.
- It cannot recover a dropped database unless native backups/PITR were configured first.
- Table snapshots are not a substitute for PostgreSQL PITR.
- Unknown or ambiguous SQL should fail closed in production-like modes