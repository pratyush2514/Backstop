# AI Agent Setup

The recommended backstop journey is:

```text
1. Start backstop gateway and sync sidecar.
2. Configure the AI tool to use backstop MCP.
3. Remove raw PostgreSQL credentials from the AI tool.
4. Give the gateway the database credentials.
5. Use audit, approval, metrics, and recovery readiness from backstop.
```

## Correct Credential Boundary

Correct:

```text
AI agent has:
- BACKSTOP_URL
- BACKSTOP_TOKEN
- BACKSTOP_AGENT_ID

backstop gateway has:
- PostgreSQL connection string
```

Wrong:

```text
AI agent has:
- PostgreSQL host/user/password
```

If the agent has raw DB credentials, it can bypass backstop and backstop becomes
recovery-only.

## Agent ID Guidance

`agent_id` answers: "who is making this database request?"

It is used for:

- audit filtering;
- approval context;
- risky retry detection;
- quarantine state.

Choose ids like:

```text
cursor-local
codex-dev-agent
analytics-agent-staging
support-agent-prod
```

Avoid:

```text
random UUID per request
user password
database URL
API token
```

## Recommended Agent Behavior

Agents should:

- use `backstop_analyze_query` before writes and schema changes;
- use `backstop_execute_query` for actual execution;
- never connect directly to PostgreSQL;
- surface approval-required responses to a human;
- avoid retrying denied dangerous operations with slightly different SQL.

Operators should:

- keep approval tools disabled for autonomous MCP clients;
- enable bypass detection in the sync sidecar;
- configure PostgreSQL least privilege;
- validate PITR/native backups before trusting full database recovery.

