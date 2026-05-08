"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { RevealSection, SectionHeading } from "./ui";

const integrations = [
  ["Databases", "PostgreSQL", "Full support - SQL interception + schema-aware restore", true],
  ["Metadata", "SQLite", "Durable local metadata store for audit, approvals, alerts, and health", true],
  ["Python SDK", "psycopg2", "Guard raw connections with backstop.guard(...)", true],
  ["Python SDK", "SQLAlchemy", "Attach engine hooks with protect_engine(...)", true],
  ["Python SDK", "Django", "Use through Django's underlying DBAPI connection", true],
  ["Agent Integration", "MCP clients", "Use @backstop/mcp-server with mode profiles and scoped tokens", true],
  ["Agent Integration", "Cursor", "Documented MCP setup with stable BACKSTOP_AGENT_ID", true],
  ["Agent Integration", "Claude Desktop", "Documented MCP setup through stdio", true],
  ["Agent Integration", "LangChain", "Route SQL tool calls through the gateway or Python SDK", true],
  ["Agent Integration", "Node SDK", "Use @backstop/client for custom apps and agents", true],
  ["Infrastructure", "AWS S3", "Snapshot manifests and Parquet data", true],
  ["Infrastructure", "MinIO", "Supported by s3://bucket@endpoint URLs", true],
  ["Infrastructure", "Prometheus", "Text metrics exposed by services", true],
  ["Infrastructure", "Docker Compose", "OSS e2e stack included", true]
] as const;

export function IntegrationCard({ group, name, status, ready, index }: { group: string; name: string; status: string; ready: boolean; index: number }) {
  const [hovered, setHovered] = useState(false);
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true }}
      transition={{ delay: index * 0.025 }}
      className={cn("relative overflow-hidden rounded-lg border border-border bg-bg-primary p-4", !ready && "cursor-default opacity-35")}
      onMouseEnter={() => ready && setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div className="text-xs uppercase tracking-[0.14em] text-text-tertiary">{group}</div>
      <div className={cn("mt-3 font-mono text-sm text-text-secondary grayscale transition duration-300", ready && hovered && "text-text-primary grayscale-0")}>{name}</div>
      <AnimatePresence>
        {hovered && (
          <motion.div
            initial={{ opacity: 0, y: "110%" }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: "110%" }}
            transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
            className="pointer-events-none absolute inset-x-0 bottom-0 border-t border-[rgba(255,68,68,0.15)] bg-bg-elevated px-4 py-2.5 text-[11px] leading-5 text-text-secondary"
          >
            <span className="font-medium text-text-primary">{name}</span> — {status}
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

export function IntegrationsSection() {
  return (
    <RevealSection className="border-y border-border bg-bg-secondary py-24">
      <SectionHeading before="Fits into your current" red="stack." />
      <div className="fade-mask mx-auto mt-12 max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {integrations.map(([group, name, status, ready], index) => (
            <IntegrationCard key={`${group}-${name}`} group={group} name={name} status={status} ready={Boolean(ready)} index={index} />
          ))}
        </div>
      </div>
    </RevealSection>
  );
}
