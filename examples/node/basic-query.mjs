import { BackstopClient } from "@backstop/client";

const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL ?? "http://localhost:8080",
  token: process.env.BACKSTOP_TOKEN ?? "dev-token",
  agentId: process.env.BACKSTOP_AGENT_ID ?? "node-basic-example",
});

const result = await backstop.query("SELECT count(*) AS users FROM users");
console.log(JSON.stringify(result, null, 2));

