"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useMotionValue, useReducedMotion } from "framer-motion";
import { IconBellRinging } from "@tabler/icons-react";
import { cn } from "@/lib/utils";
import { RevealSection, RiskBadge, SectionHeading } from "./ui";

const AGENT_ROWS = [
  { agent: "gpt-4-agent-v2",     init: "G", color: "#4ade80", sql: "SELECT * FROM orders",            risk: "SAFE",     status: "passed",      time: "2s" },
  { agent: "gpt-4-agent-v2",     init: "G", color: "#4ade80", sql: "UPDATE inventory SET qty = 0...", risk: "LOW",      status: "logged",      time: "5s" },
  { agent: "cursor-agent",        init: "C", color: "#60a5fa", sql: "DROP TABLE sessions",             risk: "CRITICAL", status: "snapshotted", time: "8s" },
  { agent: "langchain-agent-v2",  init: "L", color: "#f97316", sql: "DELETE FROM payments",           risk: "CRITICAL", status: "blocked",     time: "11s" },
];

const TIMELINE_STEPS = [
  { label: "Query Intercepted", sub: "CRITICAL risk detected in real-time",       at: "intercept" },
  { label: "Snapshot Created",  sub: "Table state preserved before any mutation", at: "intercept" },
  { label: "Human Notified",    sub: "Approval required before agent proceeds",   at: "approval"  },
];

export function AgentAvatar({ init, color }: { init: string; color: string }) {
  return (
    <span
      className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold"
      style={{ background: `${color}22`, border: `1px solid ${color}55`, color }}
    >
      {init}
    </span>
  );
}

export function TypewriterSQL({ text, done }: { text: string; done: boolean }) {
  const reduced = useReducedMotion();
  if (done || reduced) return <>{text}</>;
  return (
    <motion.span
      className="inline-block"
      initial={{ clipPath: "inset(0 100% 0 0)" }}
      animate={{ clipPath: "inset(0 0% 0 0)" }}
      transition={{ duration: Math.min(text.length * 0.032, 1.4), ease: "linear" }}
    >
      {text}
    </motion.span>
  );
}

export function AgentSection() {
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { once: true, amount: 0.3 });
  const reduced = useReducedMotion();
  const shakeX = useMotionValue(0);

  const [step, setStep] = useState(-1);
  const [intercepted, setIntercepted] = useState(false);
  const [flashing, setFlashing] = useState(false);
  const [sweeping, setSweeping] = useState(false);
  const [approvalVisible, setApprovalVisible] = useState(false);

  useEffect(() => {
    if (!inView) return;
    if (reduced) { setStep(3); setIntercepted(true); setApprovalVisible(true); return; }

    const t: ReturnType<typeof setTimeout>[] = [];
    t.push(setTimeout(() => setStep(0), 300));
    t.push(setTimeout(() => setStep(1), 1100));
    t.push(setTimeout(() => setStep(2), 1900));
    t.push(setTimeout(() => setStep(3), 2700));
    t.push(setTimeout(() => { setFlashing(true); setSweeping(true); setIntercepted(true); }, 2700));
    t.push(setTimeout(() => setFlashing(false), 2920));
    t.push(setTimeout(() => setSweeping(false), 3250));
    ([[-5, 2750],[5, 2800],[-4, 2850],[4, 2900],[-2, 2950],[2, 3000],[0, 3050]] as [number,number][]).forEach(
      ([x, ms]) => t.push(setTimeout(() => shakeX.set(x), ms))
    );
    t.push(setTimeout(() => setApprovalVisible(true), 3500));
    return () => t.forEach(clearTimeout);
  }, [inView, reduced, shakeX]);

  return (
    <RevealSection className="bg-bg-secondary py-24">
      <div ref={ref} className="mx-auto grid max-w-7xl gap-12 px-4 sm:px-6 lg:grid-cols-[0.85fr_1.15fr] lg:items-start lg:px-8">

        {/* Left: text + 3-act timeline */}
        <div>
          <SectionHeading before="Built for the age of" red="agents." align="left" />
          <p className="mt-6 max-w-xl leading-8 text-text-secondary">
            AI agents — LangChain, LlamaIndex, Cursor, any OpenAI function-calling agent — can be tagged with an actor
            identity when wrapped with{" "}
            <code className="font-mono text-danger">{`backstop.guard(conn, actor="langchain-agent-v2", storage="s3://...")`}</code>.
          </p>
          <p className="mt-5 max-w-xl leading-8 text-text-secondary">
            Risky actions are attributed and audited. The gateway can require human approval for HIGH and CRITICAL
            operations, and table-level destructive actions can be bound to verified recovery points before they touch
            production data.
          </p>

          <div className="mt-10">
            {TIMELINE_STEPS.map((ts, i) => {
              const active = ts.at === "intercept" ? intercepted : approvalVisible;
              return (
                <div key={ts.label} className="flex gap-4">
                  <div className="flex flex-col items-center">
                    <motion.div
                      className="mt-1 h-3 w-3 rounded-full border-2"
                      animate={{
                        borderColor: active ? "var(--danger)" : "var(--border)",
                        backgroundColor: active ? "var(--danger)" : "transparent",
                        boxShadow: active ? "0 0 10px rgba(255,68,68,0.55)" : "none",
                      }}
                      transition={{ duration: 0.4 }}
                    />
                    {i < 2 && (
                      <motion.div
                        className="w-px flex-none"
                        style={{ height: 44 }}
                        animate={{ backgroundColor: active ? "rgba(255,68,68,0.5)" : "var(--border)" }}
                        transition={{ duration: 0.4 }}
                      />
                    )}
                  </div>
                  <div className={cn("pb-8 pt-0.5", i === 2 && "pb-0")}>
                    <motion.p
                      className="text-sm font-semibold"
                      animate={{ color: active ? "var(--text-primary)" : "var(--text-tertiary)" }}
                      transition={{ duration: 0.4 }}
                    >
                      {ts.label}
                    </motion.p>
                    <p className="mt-0.5 text-xs text-text-tertiary">{ts.sub}</p>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Right: 3D perspective terminal */}
        <div className="relative" style={{ perspective: "1400px" }}>
          {/* Diffused glow pool */}
          <div className="pointer-events-none absolute -bottom-10 -left-6 -right-6 h-28 rounded-full bg-[radial-gradient(ellipse_at_center,rgba(255,68,68,0.12),transparent_70%)] blur-3xl" />

          <motion.div
            style={{
              x: shakeX,
              transformStyle: "preserve-3d" as const,
              boxShadow: "0 28px 80px rgba(0,0,0,0.55), 0 0 0 1px rgba(255,68,68,0.07), inset 0 1px 0 rgba(255,255,255,0.04)",
            }}
            initial={{ rotateY: -14, rotateX: 3 }}
            whileInView={{ rotateY: -7, rotateX: 1.5 }}
            whileHover={{ rotateY: -3, rotateX: 0 }}
            viewport={{ once: true, amount: 0.2 }}
            transition={{ duration: 0.9, ease: [0.16, 1, 0.3, 1] }}
            className="overflow-hidden rounded-xl border border-border bg-bg-primary"
          >
            {/* macOS-style chrome bar */}
            <div className="flex items-center justify-between border-b border-border bg-bg-elevated px-5 py-3">
              <div className="flex items-center gap-3">
                <div className="flex gap-1.5">
                  {["#ff5f56","#ffbd2e","#27c93f"].map(c => (
                    <div key={c} className="h-2.5 w-2.5 rounded-full" style={{ background: c }} />
                  ))}
                </div>
                <span className="font-mono text-xs text-text-tertiary">agent audit log</span>
              </div>
              <div className="flex items-center gap-1.5">
                <motion.div
                  className="h-2 w-2 rounded-full bg-success"
                  animate={{ opacity: [1, 0.3, 1] }}
                  transition={{ duration: 1.8, repeat: Infinity, ease: "easeInOut" }}
                />
                <span className="font-mono text-[11px] text-text-tertiary">live</span>
              </div>
            </div>

            {/* Column headers */}
            <div className="grid grid-cols-[1.3fr_1.9fr_0.65fr_1fr_0.4fr] gap-2 border-b border-border px-5 py-2.5 text-[11px] font-medium uppercase tracking-[0.1em] text-text-tertiary">
              {["Agent","Query","Risk","Status","Time"].map(h => <span key={h}>{h}</span>)}
            </div>

            {/* Rows */}
            <div className="relative">
              {/* Red flash overlay */}
              <AnimatePresence>
                {flashing && (
                  <motion.div
                    key="flash"
                    className="pointer-events-none absolute inset-0 z-30"
                    style={{ background: "rgba(255,68,68,0.09)" }}
                    initial={{ opacity: 1 }}
                    animate={{ opacity: 0 }}
                    transition={{ duration: 0.22 }}
                  />
                )}
              </AnimatePresence>

              {/* Horizontal sweep line */}
              <AnimatePresence>
                {sweeping && (
                  <motion.div
                    key="sweep"
                    className="pointer-events-none absolute left-0 right-0 z-30 h-px origin-left bg-danger"
                    style={{ top: "72%" }}
                    initial={{ scaleX: 0 }}
                    animate={{ scaleX: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.28, ease: "linear" }}
                  />
                )}
              </AnimatePresence>

              {AGENT_ROWS.map((row, i) => {
                const visible = step >= i;
                const active = step === i;
                const done = step > i;
                const isBlocked = i === 3;
                const dimmed = intercepted && !isBlocked;

                return (
                  <AnimatePresence key={i}>
                    {visible && (
                      <motion.div
                        initial={{ opacity: 0, y: -10 }}
                        animate={{ opacity: dimmed ? 0.18 : 1, y: 0 }}
                        transition={{ duration: 0.35, ease: [0.16, 1, 0.3, 1] }}
                        className={cn(
                          "grid grid-cols-[1.3fr_1.9fr_0.65fr_1fr_0.4fr] items-center gap-2 px-5 py-3.5 font-mono text-xs",
                          i < AGENT_ROWS.length - 1 && "border-b border-border",
                          isBlocked && intercepted && "bg-[rgba(255,68,68,0.05)]",
                        )}
                      >
                        <span className="flex min-w-0 items-center gap-2 overflow-hidden">
                          <AgentAvatar init={row.init} color={row.color} />
                          <span className="truncate text-text-secondary">{row.agent}</span>
                        </span>
                        <span className="min-w-0 overflow-hidden text-text-secondary">
                          <TypewriterSQL text={row.sql} done={done} />
                          {active && <span className="terminal-cursor ml-0.5" />}
                        </span>
                        <motion.span
                          initial={{ scale: 0, opacity: 0 }}
                          animate={{ scale: 1, opacity: 1 }}
                          transition={{ delay: 0.5, type: "spring", stiffness: 480, damping: 20 }}
                        >
                          <RiskBadge risk={row.risk} />
                        </motion.span>
                        <motion.span
                          initial={{ opacity: 0 }}
                          animate={{ opacity: 1 }}
                          transition={{ delay: 0.7 }}
                          className={cn("text-text-tertiary", isBlocked && intercepted && "font-semibold text-danger")}
                        >
                          {row.status}
                        </motion.span>
                        <motion.span
                          initial={{ opacity: 0 }}
                          animate={{ opacity: 1 }}
                          transition={{ delay: 0.6 }}
                          className="text-right text-text-tertiary"
                        >
                          {row.time}
                        </motion.span>
                      </motion.div>
                    )}
                  </AnimatePresence>
                );
              })}
            </div>

            {/* Approval card */}
            <AnimatePresence>
              {approvalVisible && (
                <motion.div
                  initial={{ opacity: 0, y: 18, scale: 0.97 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  transition={{ duration: 0.45, ease: [0.16, 1, 0.3, 1] }}
                  className="mx-4 mb-4 mt-3 overflow-hidden rounded-lg border border-[rgba(255,68,68,0.35)] bg-[rgba(255,68,68,0.06)]"
                >
                  <div className="h-px w-full bg-gradient-to-r from-transparent via-danger to-transparent opacity-40" />
                  <div className="flex items-start gap-3 p-4">
                    <motion.div
                      animate={{ rotate: [0, -12, 12, -8, 8, -4, 4, 0] }}
                      transition={{ duration: 0.55, delay: 0.15 }}
                    >
                      <IconBellRinging className="mt-0.5 shrink-0 text-danger" size={18} stroke={1.5} />
                    </motion.div>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-semibold text-text-primary">Approval required</p>
                      <p className="mt-0.5 text-xs leading-5 text-text-secondary">
                        Agent <span className="font-mono text-danger">langchain-agent-v2</span> wants to{" "}
                        <span className="font-mono text-warning">DELETE FROM payments</span> — operator notified.
                      </p>
                    </div>
                    <div className="flex shrink-0 gap-2">
                      <button className="rounded-md bg-[rgba(40,167,69,0.15)] px-3 py-1.5 text-xs font-semibold text-success transition-colors hover:bg-[rgba(40,167,69,0.25)]">
                        Approve
                      </button>
                      <button className="rounded-md bg-[rgba(255,68,68,0.12)] px-3 py-1.5 text-xs font-semibold text-danger transition-colors hover:bg-[rgba(255,68,68,0.22)]">
                        Deny
                      </button>
                    </div>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
          </motion.div>
        </div>

      </div>
    </RevealSection>
  );
}
