"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import anime from "animejs";
import { IconAlertTriangle, IconArrowRight, IconCheck } from "@tabler/icons-react";
import dynamic from "next/dynamic";
import { cn, formatNumber } from "@/lib/utils";
import { BlurWords, GhostButton, GlowButton, ease } from "./ui";

const AuroraBg = dynamic(() => import("@/components/aurora-bg"), { ssr: false });

const queries = [
  { sql: "DROP TABLE users;", rows: 1842933, snap: "snap_a3f9", table: "users" },
  { sql: "DELETE FROM payments;", rows: 432118, snap: "snap_b7e2", table: "payments" },
  { sql: "TRUNCATE orders;", rows: 982044, snap: "snap_f41c", table: "orders" }
];

export function IncidentBadge() {
  return (
    <a
      href="https://www.theregister.com/2026/04/27/cursoropus_agent_snuffs_out_pocketos/"
      target="_blank"
      rel="noopener noreferrer"
      className="shimmer relative inline-flex w-full max-w-[calc(100vw-32px)] items-center gap-2 overflow-hidden rounded-full border border-[rgba(255,68,68,0.25)] bg-[rgba(255,68,68,0.08)] px-3 py-1.5 text-sm text-[#ff8080] transition hover:border-[rgba(255,68,68,0.45)] sm:w-fit"
    >
      <IconAlertTriangle size={16} stroke={1.5} />
      <span className="truncate">Apr 25 — AI agent deleted PocketOS's entire production DB in 9 seconds. →</span>
    </a>
  );
}

export function HeroTerminal() {
  const [queryIndex, setQueryIndex] = useState(0);
  const [typed, setTyped] = useState("");
  const [phase, setPhase] = useState<"typing" | "intercept" | "capture" | "saved" | "choice">("typing");
  const [decision, setDecision] = useState<"idle" | "blocked" | "allowed">("idle");
  const progressRef = useRef<HTMLDivElement>(null);
  const reduced = useReducedMotion();
  const query = queries[queryIndex];

  useEffect(() => {
    let cancelled = false;
    const timers: ReturnType<typeof setTimeout>[] = [];
    const wait = (ms: number, fn: () => void) => timers.push(setTimeout(fn, ms));

    function run() {
      if (cancelled) return;
      setTyped("");
      setPhase("typing");
      setDecision("idle");
      if (progressRef.current) {
        progressRef.current.style.transform = "scaleX(0)";
      }

      if (reduced) {
        setTyped(query.sql);
        setPhase("choice");
        wait(3200, next);
        return;
      }

      wait(500, () => {
        query.sql.split("").forEach((char, index) => {
          wait(index * 40, () => setTyped((current) => current + char));
        });
        wait(query.sql.length * 40 + 180, () => setPhase("intercept"));
        wait(query.sql.length * 40 + 520, () => {
          setPhase("capture");
          if (progressRef.current) {
            anime({
              targets: progressRef.current,
              scaleX: [0, 1],
              duration: 1200,
              easing: "cubicBezier(0.4, 0, 0.2, 1)"
            });
          }
        });
        wait(query.sql.length * 40 + 1850, () => setPhase("saved"));
        wait(query.sql.length * 40 + 2250, () => setPhase("choice"));
        wait(8000, next);
      });
    }

    function next() {
      setQueryIndex((current) => (current + 1) % queries.length);
    }

    run();
    return () => {
      cancelled = true;
      timers.forEach(clearTimeout);
    };
  }, [query.sql, reduced]);

  return (
    <div
      className="scanlines relative mx-auto w-[min(100%,calc(100vw-32px))] max-w-full overflow-hidden rounded-xl border border-border bg-bg-secondary shadow-dangerSoft lg:max-w-2xl"
      aria-live="polite"
    >
      <div className="flex items-center justify-between border-b border-border bg-[rgba(22,27,34,0.75)] px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="h-2.5 w-2.5 rounded-full bg-danger" />
          <span className="h-2.5 w-2.5 rounded-full bg-warning" />
          <span className="h-2.5 w-2.5 rounded-full bg-success" />
        </div>
        <div className="font-mono text-xs text-text-secondary">production-db</div>
        <div className="flex items-center gap-2 font-mono text-xs text-danger">
          <span className="h-1.5 w-1.5 rounded-full bg-danger shadow-[0_0_10px_rgba(255,68,68,0.9)]" />
          LIVE
        </div>
      </div>
      <div className="relative min-h-[430px] p-5 font-mono text-sm sm:p-7">
        <p className="text-text-tertiary">&gt; agent-gpt4 executing:</p>
        <div className={cn("mt-5 text-[clamp(20px,4vw,32px)] leading-tight text-text-primary transition-colors", phase !== "typing" && "text-danger")}>
          {typed}
          {phase === "typing" && <span className="terminal-cursor" />}
        </div>
        <AnimatePresence>
          {phase !== "typing" && (
            <motion.div
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.3 }}
              className="mt-8 inline-flex rounded-md border border-[rgba(255,68,68,0.4)] bg-[rgba(255,68,68,0.08)] px-2.5 py-1 text-danger"
            >
              [backstop intercepted]
            </motion.div>
          )}
        </AnimatePresence>
        {["capture", "saved", "choice"].includes(phase) && (
          <div className="mt-7">
            <div className="mb-2 flex items-center justify-between text-xs text-text-secondary">
              <span className="min-w-0 truncate">verifying latest recovery point for {query.table}...</span>
              <span className="hidden shrink-0 pl-3 sm:inline">AST: CRITICAL</span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-bg-elevated">
              <div
                ref={progressRef}
                className="h-full origin-left rounded-full bg-gradient-to-r from-danger to-success shadow-[0_0_18px_rgba(255,68,68,0.35)]"
                style={{ transform: "scaleX(0)" }}
              />
            </div>
          </div>
        )}
        {["saved", "choice"].includes(phase) && (
          <motion.div initial={{ opacity: 0, scale: 0.5 }} animate={{ opacity: 1, scale: 1 }} className="mt-7 flex items-start gap-3 text-success">
            <IconCheck size={22} stroke={1.5} />
            <div>
              <div>{query.snap} verified → s3://your-bucket/snapshots/</div>
              <div className="mt-1 text-xs text-text-tertiary">
                {query.table} / latest sidecar snapshot / manifest verified
              </div>
            </div>
          </motion.div>
        )}
        {phase === "choice" && (
          <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="mt-8 flex flex-wrap gap-3">
            <button
              onClick={() => setDecision("blocked")}
              className={cn("rounded-md border border-[rgba(255,68,68,0.5)] bg-danger px-4 py-2 text-xs font-semibold text-white", decision === "blocked" && "demo-pulse")}
              aria-label="Block destructive query"
            >
              BLOCK
            </button>
            <button
              onClick={() => setDecision("allowed")}
              className={cn("rounded-md border border-[rgba(40,167,69,0.45)] bg-[rgba(40,167,69,0.12)] px-4 py-2 text-xs font-semibold text-success", decision === "allowed" && "demo-pulse")}
              aria-label="Allow query after snapshot"
            >
              ALLOW + VERIFY
            </button>
          </motion.div>
        )}
        <AnimatePresence>
          {decision !== "idle" && (
            <motion.div
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -6 }}
              className={cn(
                "mt-4 rounded-lg border p-3 text-xs",
                decision === "blocked"
                  ? "border-[rgba(255,68,68,0.45)] bg-[rgba(255,68,68,0.07)] text-danger"
                  : "border-[rgba(40,167,69,0.45)] bg-[rgba(40,167,69,0.07)] text-success"
              )}
            >
              {decision === "blocked"
                ? `Policy action: block. ${query.sql.replace(";", "")} never reaches production.`
                : `Policy action: execute. ${query.snap} is the latest verified recovery point, audit event recorded, query may proceed.`}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}

export function Hero() {
  const reduced = useReducedMotion();
  return (
    <section id="top" className="relative min-h-screen overflow-hidden pt-[60px]">
      <AuroraBg
        colorStops={["#FF4444", "#080C10", "#0066FF"]}
        amplitude={1.2}
        blend={0.5}
        speed={1.0}
        className="absolute inset-0 mix-blend-screen"
      />
      <div className="relative mx-auto grid min-h-[calc(100vh-60px)] max-w-7xl min-w-0 items-center gap-12 px-4 py-16 sm:px-6 lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)] lg:px-8">
        <motion.div
          initial={reduced ? { opacity: 1 } : { opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5, ease }}
          className="w-full min-w-0 max-w-[calc(100vw-32px)] lg:max-w-3xl"
        >
          <IncidentBadge />
          <h1 className="serif-headline mt-7 max-w-full lg:max-w-[850px]">
            <BlurWords text="The last line of" delay={0.1} />
            <br />
            <BlurWords text="defense between" delay={0.2} />
            <br />
            <BlurWords text="your agent and" delay={0.3} />
            <br />
            <motion.span
              className="red-italic"
              initial={{ opacity: 0, filter: "blur(12px)", y: 6 }}
              whileInView={{ opacity: 1, filter: "blur(0px)", y: 0 }}
              viewport={{ once: true, amount: 0 }}
              transition={{ duration: 0.55, ease: [0.16, 1, 0.3, 1], delay: 0.4 }}
              style={{ display: "inline-block" }}
            >
              production.
            </motion.span>
          </h1>
          <motion.p
            initial={reduced ? { opacity: 1 } : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.25, duration: 0.45, ease }}
            className="mt-6 max-w-2xl text-[18px] leading-8 text-text-secondary"
          >
            Route agent SQL through a PostgreSQL-aware gateway. Risky writes can require approval, verified recovery points,
            and auditable restore paths before they touch production.
          </motion.p>
          <motion.div
            initial={reduced ? { opacity: 1 } : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.38, duration: 0.45, ease }}
            className="mt-8 flex flex-col gap-3 sm:flex-row"
          >
            <GlowButton href="#pricing" className="w-full justify-center sm:w-auto">Start free — no card required</GlowButton>
            <GhostButton href="https://www.theregister.com/2026/04/27/cursoropus_agent_snuffs_out_pocketos/" target="_blank" rel="noopener noreferrer">
              Read the incident <IconArrowRight size={18} stroke={1.5} />
            </GhostButton>
          </motion.div>
          <p className="mt-5 text-xs text-text-tertiary">Your data stays in your infrastructure · Open source core · Self-hosted by default</p>
        </motion.div>
        <motion.div
          initial={reduced ? { opacity: 1 } : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ delay: 0.2, duration: 0.7, ease }}
          className="min-w-0"
        >
          <HeroTerminal />
        </motion.div>
      </div>
    </section>
  );
}
