# backstop Node SDK

The Node SDK is for custom agent applications that generate SQL and need to send
that SQL through backstop instead of connecting directly to PostgreSQL.

```text
Node agent -> @backstop/client -> backstop gateway -> PostgreSQL
```

## Install

For local workspace development:

```powershell
npm install
npm run build --workspace @backstop/client
```

For a published release, the intended install shape is:

```powershell
npm install @backstop/client
```

## Basic Usage

```ts
import { BackstopClient } from "@backstop/client";

const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL ?? "http://localhost:8080",
  token: process.env.BACKSTOP_TOKEN,
  agentId: process.env.BACKSTOP_AGENT_ID ?? "billing-agent-dev",
});

const result = await backstop.query("SELECT count(*) FROM users");
console.log(result);
```

## Where Does `agentId` Come From?

`agentId` is not issued by backstop. It is a stable identity chosen by the
developer or operator so backstop can write useful audit logs, approval records,
and quarantine state.

Good examples:

```text
cursor-local
codex-dev-agent
claude-desktop-staging
customer-support-agent
billing-agent-prod
```

Rules of thumb:

- Use one stable id per agent/app/environment.
- Do not use random ids per request; that breaks audit history and quarantine.
- Do not put secrets in the id.
- Prefer setting `BACKSTOP_AGENT_ID` in config so code does not need to hard-code it.

## Analyze Before Execute

```ts
const analysis = await backstop.analyzeQuery("DELETE FROM users WHERE id > 0");
console.log(analysis.safety_metadata);
```

`analyzeQuery` never executes SQL. It returns backstop's risk and policy decision.

## Approval And Policy Results

By default, `executeQuery` returns the gateway result even when the query is
blocked or needs approval. If you want typed exceptions, opt in:

```ts
await backstop.executeQuery("DROP DATABASE appdb", {
  throwOnPolicyViolation: true,
});
```

Typed errors include:

```text
BackstopAuthError
BackstopNetworkError
BackstopTimeoutError
BackstopPolicyBlockedError
BackstopApprovalRequiredError
BackstopRecoveryNotReadyError
```

## Other Methods

```ts
await backstop.listSnapshots({ table: "users" });
await backstop.prepareRestoreSnapshot("snap_1234", "users");
await backstop.getPendingApprovals();
await backstop.approve("appr_1234");
await backstop.deny("appr_1234");
await backstop.getAudit({ agentId: "billing-agent-dev" });
await backstop.getAlerts();
await backstop.getHealth();
await backstop.getMetrics();
```

Approval methods should be used by operator/admin flows, not by autonomous
agents unless you explicitly trust that agent to approve its own writes.
`prepareRestoreSnapshot` returns a secret-safe restore plan and never includes
the raw database connection string in the SDK/MCP response.

