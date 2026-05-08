"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import { cn } from "@/lib/utils";
import { RevealSection, RiskBadge, SectionHeading } from "./ui";

const riskRows = [
  { sql: "SELECT * FROM users WHERE id = 5", risk: "SAFE" },
  { sql: "INSERT INTO logs (event) VALUES ('login')", risk: "HIGH" },
  { sql: "UPDATE users SET name = 'Ana' WHERE id = 7", risk: "HIGH" },
  { sql: "DELETE FROM sessions WHERE expired = true", risk: "HIGH" },
  { sql: "ALTER TABLE users DROP COLUMN email", risk: "HIGH" },
  { sql: "DELETE FROM payments", risk: "CRITICAL" },
  { sql: "DROP TABLE users", risk: "CRITICAL" },
  { sql: "EXPLAIN SELECT * FROM orders", risk: "SAFE" }
];

type RiskRow = { sql: string; risk: string; seq: number };

const RISK_VISIBLE = 7;

export function RiskEngineSection() {
  const feedRef = useRef<HTMLDivElement>(null);
  const inView = useInView(feedRef, { once: false, amount: 0.35 });
  const reduced = useReducedMotion();

  const [rows, setRows] = useState<RiskRow[]>(() =>
    [...riskRows].reverse().slice(0, RISK_VISIBLE).map((r, i) => ({ ...r, seq: i }))
  );
  const cursorRef = useRef(RISK_VISIBLE);
  const seqRef = useRef(50);

  useEffect(() => {
    if (!inView || reduced) return;
    // Let the reveal animation settle before cycling starts
    const warmup = setTimeout(() => {
      const id = setInterval(() => {
        const next = riskRows[cursorRef.current % riskRows.length];
        cursorRef.current++;
        setRows(prev => [{ ...next, seq: seqRef.current++ }, ...prev].slice(0, RISK_VISIBLE));
      }, 2400);
      return () => clearInterval(id);
    }, 900);
    return () => clearTimeout(warmup);
  }, [inView, reduced]);

  return (
    <RevealSection className="py-24">
      <SectionHeading before="Every query. Classified." red="Instantly." />
      <div
        ref={feedRef}
        className="scanlines relative mx-auto mt-12 max-w-4xl overflow-hidden rounded-xl border border-border bg-bg-secondary p-5 font-mono text-sm"
      >
        {/* Header */}
        <div className="mb-4 flex items-center justify-between border-b border-border pb-3 text-xs text-text-tertiary">
          <div className="flex items-center gap-2">
            <motion.span
              className="h-1.5 w-1.5 rounded-full bg-danger"
              animate={inView && !reduced ? { opacity: [1, 0.3, 1], scale: [1, 0.8, 1] } : { opacity: 1 }}
              transition={{ duration: 1.8, repeat: Infinity, ease: "easeInOut" }}
            />
            query feed
          </div>
          <span>risk level</span>
        </div>

        {/* Feed rows */}
        <div className="space-y-2">
          <AnimatePresence initial={false} mode="popLayout">
            {rows.map((row, index) => {
              const isNewest = index === 0;
              const isCritical = row.risk === "CRITICAL";
              return (
                <motion.div
                  key={row.seq}
                  layout="position"
                  initial={{ opacity: 0, filter: "blur(4px)", y: -6 }}
                  animate={{ opacity: 1, filter: "blur(0px)", y: 0 }}
                  exit={{ opacity: 0, filter: "blur(2px)", transition: { duration: 0.18 } }}
                  transition={{ duration: 0.42, ease: [0.16, 1, 0.3, 1] }}
                  className={cn(
                    "flex items-center justify-between gap-4 rounded-lg border px-4 py-3 transition-colors duration-500",
                    isNewest && !isCritical && "border-[rgba(255,68,68,0.18)] bg-bg-elevated",
                    isNewest && isCritical  && "border-[rgba(255,68,68,0.35)] bg-[rgba(255,68,68,0.05)]",
                    !isNewest             && "border-border bg-bg-primary",
                  )}
                >
                  <span className={cn(
                    "truncate transition-colors duration-300",
                    isNewest ? "text-text-primary" : "text-text-secondary",
                  )}>
                    {row.sql}
                  </span>
                  <RiskBadge risk={row.risk} />
                </motion.div>
              );
            })}
          </AnimatePresence>
        </div>
      </div>
    </RevealSection>
  );
}
