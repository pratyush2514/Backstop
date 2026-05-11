import { BackstopClient } from "@backstop/client";

const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL ?? "http://localhost:8080",
  token: process.env.BACKSTOP_TOKEN ?? "dev-token",
  agentId: process.env.BACKSTOP_AGENT_ID ?? "node-analysis-example",
});

const sql = "DELETE FROM users WHERE id > 0";
const analysis = await backstop.analyzeQuery(sql);
console.log("analysis");
console.log(JSON.stringify(analysis, null, 2));

if (analysis.safety_metadata?.policy_action === "execute") {
  const result = await backstop.executeQuery(sql);
  console.log("executed");
  console.log(JSON.stringify(result, null, 2));
} else {
  console.log("not executing automatically; backstop requires approval or blocks this query");
}

