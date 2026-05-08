"use client";

import { motion } from "framer-motion";
import { IconBrandX } from "@tabler/icons-react";
import { RevealSection, SectionHeading } from "./ui";

const TWEETS = [
  {
    handle: "pattern://cursor-mcp",
    name: "Cursor via MCP",
    role: "Common rollout pattern",
    initials: "CM",
    color: "#FF7B72",
    text: "Give the AI tool the Backstop MCP server instead of DATABASE_URL. The agent gets SQL tools, but the gateway keeps approval, audit, and recovery decisions in the middle.",
    time: "Recommended setup",
  },
  {
    handle: "pattern://gateway-approvals",
    name: "Approval workflow",
    role: "Common rollout pattern",
    initials: "AW",
    color: "#79C0FF",
    text: "Use agent-scoped tokens for execution and operator-scoped tokens for approve or deny. Autonomous agents should be able to request risky work, but not approve their own destructive queries.",
    time: "Recommended setup",
  },
  {
    handle: "pattern://audit-attribution",
    name: "Audit attribution",
    role: "Common rollout pattern",
    initials: "AA",
    color: "#56D364",
    text: "Stable BACKSTOP_AGENT_ID values make the audit trail readable. They also enable agent quarantine, filtered review, and cleaner incident response when multiple tools touch the same database.",
    time: "Operational value",
  },
  {
    handle: "pattern://table-restore",
    name: "Table recovery",
    role: "Common rollout pattern",
    initials: "TR",
    color: "#F0883E",
    text: "Use sidecar snapshots for fast table-level recovery, and keep native PostgreSQL backup plus WAL/PITR for full-database incidents. Backstop is strongest when those two planes are used together.",
    time: "Operational value",
  },
  {
    handle: "pattern://storage-byos",
    name: "Bring your own storage",
    role: "Common rollout pattern",
    initials: "BS",
    color: "#BC8CFF",
    text: "Point snapshots and WAL artifacts at your own S3-compatible storage, such as MinIO. The safety and recovery flow stays inside infrastructure you already control.",
    time: "Infrastructure fit",
  },
  {
    handle: "pattern://docs-runbooks",
    name: "Production readiness",
    role: "Common rollout pattern",
    initials: "PR",
    color: "#FF7B72",
    text: "Run the doctor commands, snapshot drills, storage checks, and incident runbooks before rollout. Backstop adds safety value when the operational boundary is understood and rehearsed.",
    time: "Operational value",
  },
  {
    handle: "pattern://bypass-detection",
    name: "Bypass detection",
    role: "Common rollout pattern",
    initials: "BD",
    color: "#79C0FF",
    text: "If an agent or script connects directly to PostgreSQL, Backstop cannot intercept that query. Bypass detection makes this posture visible so teams do not confuse recovery-only mode with prevention.",
    time: "Boundary to know",
  },
  {
    handle: "pattern://policy-modes",
    name: "Dev vs prod policy",
    role: "Common rollout pattern",
    initials: "DP",
    color: "#56D364",
    text: "Use looser policy in development, stricter policy in production, and explicit pause or quarantine controls for incidents. That balance reduces bypass pressure without giving away protection.",
    time: "Rollout advice",
  },
  {
    handle: "pattern://metadata-sqlite",
    name: "Local-first metadata",
    role: "Common rollout pattern",
    initials: "LM",
    color: "#F0883E",
    text: "SQLite metadata keeps the OSS core easy to run locally while still giving you durable audit, approval, alert, and snapshot records that a future dashboard can read directly.",
    time: "Architecture value",
  },
];

const COLUMN_INDICES = [
  [0, 3, 6],
  [1, 4, 7],
  [2, 5, 8],
];

function TweetCard({
  tweet,
  delay,
}: {
  tweet: (typeof TWEETS)[number];
  delay: number;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, amount: 0.15 }}
      transition={{ duration: 0.45, ease: [0.16, 1, 0.3, 1], delay }}
      className="rounded-xl border border-border bg-bg-secondary p-5 transition-colors duration-200 hover:border-[rgba(255,68,68,0.2)] hover:bg-bg-elevated"
    >
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full font-mono text-[11px] font-bold"
            style={{ background: `${tweet.color}18`, color: tweet.color, border: `1px solid ${tweet.color}30` }}
          >
            {tweet.initials}
          </div>
          <div className="min-w-0">
            <p className="text-[13px] font-medium leading-none text-text-primary">{tweet.name}</p>
            <p className="mt-1 text-[11px] leading-none text-text-tertiary">{tweet.role}</p>
          </div>
        </div>
        <IconBrandX size={15} stroke={1.5} className="shrink-0 text-text-tertiary/50" />
      </div>

      {/* Body */}
      <p className="mt-4 text-[13px] leading-[1.7] text-text-secondary">{tweet.text}</p>

      {/* Footer */}
      <div className="mt-4 flex items-center justify-between border-t border-border pt-3">
        <span className="font-mono text-[10px] text-text-tertiary">{tweet.handle}</span>
        <span className="font-mono text-[10px] text-text-tertiary">{tweet.time}</span>
      </div>
    </motion.div>
  );
}

export function TestimonialsSection() {
  return (
    <RevealSection className="py-24">
      <SectionHeading before="How teams usually make" red="use of it." />

      <div className="mx-auto mt-14 grid max-w-7xl grid-cols-1 gap-4 px-4 sm:px-6 md:grid-cols-2 lg:grid-cols-3 lg:px-8">
        {COLUMN_INDICES.map((colIndices, colIdx) => (
          <div key={colIdx} className="flex flex-col gap-4">
            {colIndices.map((tweetIdx, rowIdx) => (
              <TweetCard
                key={TWEETS[tweetIdx].handle}
                tweet={TWEETS[tweetIdx]}
                delay={colIdx * 0.08 + rowIdx * 0.06}
              />
            ))}
          </div>
        ))}
      </div>
    </RevealSection>
  );
}
