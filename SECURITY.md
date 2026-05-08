# Security Policy

backstop is security-sensitive software because it sits between AI agents and
databases.

## Supported Versions

The project is currently pre-1.0. Security fixes target the latest published
alpha unless a release branch is explicitly documented.

## Reporting Vulnerabilities

Please do not open a public issue for exploitable security bugs.

Until a dedicated security contact is published for the project, report through
the repository's private security advisory flow if available. Include:

- affected component: gateway, sync, Python CLI, Node SDK, MCP server, docs;
- impact and reproduction steps;
- whether the issue allows bypass, unauthorized approval, credential exposure,
  recovery metadata corruption, or destructive query execution.

## Security Boundaries

Expected safe deployment:

```text
AI agent -> backstop MCP/SDK/gateway -> PostgreSQL
```

Unsafe deployment:

```text
AI agent -> PostgreSQL directly
```

If agents or users have direct database credentials, backstop prevention can be
bypassed. In that case, backstop becomes recovery and detection support only.

## Default Security Posture

- Gateway auth is required unless insecure mode is explicitly enabled.
- `DROP DATABASE` and `DROP SCHEMA` are blocked by default.
- Unknown SQL and parser failures fail closed.
- Approval tools in the MCP server are disabled by default.
- Secrets should not be stored in `agent_id`, audit text, or config examples.

