#!/usr/bin/env node
import { BackstopClient, scrubSecrets } from "@backstop/client";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { loadConfig, usage } from "./config.js";
import { createBackstopMcpServer } from "./server.js";

async function main(): Promise<void> {
  if (process.argv.includes("--help") || process.argv.includes("-h")) {
    process.stderr.write(`${usage()}\n`);
    return;
  }

  const config = loadConfig();
  const client = new BackstopClient({
    url: config.backstopUrl,
    token: config.backstopToken,
    agentId: config.agentId,
    timeoutMs: config.timeoutMs,
  });
  const server = createBackstopMcpServer(client, config);
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`backstop-mcp failed: ${scrubSecrets(message)}\n`);
  process.exitCode = 1;
});

