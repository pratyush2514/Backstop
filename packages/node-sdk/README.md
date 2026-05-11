# @backstop/client

TypeScript/Node client for backstop gateway.

```ts
import { BackstopClient } from "@backstop/client";

const backstop = await BackstopClient.local({
  postgresUrl: process.env.POSTGRES_URL,
  agentId: process.env.BACKSTOP_AGENT_ID ?? "my-ai-agent",
});

const result = await backstop.query("SELECT count(*) FROM users");
```

Operator/admin clients can also prepare a secret-safe restore plan:

```ts
const plan = await backstop.prepareRestoreSnapshot("snap_1234", "users");
```

The restore plan uses `BACKSTOP_RESTORE_DB`; Backstop never returns the raw
database password to the SDK or MCP client.

For local PostgreSQL development on `localhost` / `127.0.0.1`, the managed
runtime automatically adds `sslmode=disable` if you did not specify an
`sslmode` already. That avoids the common "SSL is not enabled on the server"
startup failure for default local Postgres installs.

If the first managed-local startup fails, the thrown error includes the log
path and the tail of the relevant runtime log so users do not have to guess
which process failed.

`agentId` is a stable identity chosen by the application developer. Use a value
that lets operators recognize the caller in audit logs, approvals, and
quarantine records, for example `cursor-local`, `codex-dev-agent`, or
`billing-agent`.

If you already run Backstop separately, the SDK still supports the explicit URL
form:

```ts
const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL!,
  token: process.env.BACKSTOP_TOKEN,
  agentId: "billing-agent",
});
```

