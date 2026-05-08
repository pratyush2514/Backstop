# @backstop/client

TypeScript/Node client for backstop gateway.

```ts
import { BackstopClient } from "@backstop/client";

const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL ?? "http://localhost:8080",
  token: process.env.BACKSTOP_TOKEN,
  agentId: process.env.BACKSTOP_AGENT_ID ?? "my-ai-agent",
});

const result = await backstop.query("SELECT count(*) FROM users");
```

`agentId` is a stable identity chosen by the application developer. Use a value
that lets operators recognize the caller in audit logs, approvals, and
quarantine records, for example `cursor-local`, `codex-dev-agent`, or
`billing-agent`.

