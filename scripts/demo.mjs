#!/usr/bin/env node

const lines = [
  "",
  "Backstop local demo path",
  "",
  "1. Start the full local stack:",
  "   docker compose -f deploy/docker-compose.yml up -d --build",
  "",
  "2. Prove the normal OSS lifecycle:",
  "   npm run e2e",
  "",
  "3. Prove PostgreSQL PITR/WAL recovery:",
  "   npm run e2e:pitr",
  "",
  "4. Useful local URLs:",
  "   Gateway health: http://localhost:8080/health",
  "   Sync health:    http://localhost:9091/health",
  "   MinIO console:  http://localhost:9001",
  "",
  "5. If ports are already in use, run scripts/e2e.ps1 with alternate ports.",
  "",
];

process.stdout.write(`${lines.join("\n")}\n`);
