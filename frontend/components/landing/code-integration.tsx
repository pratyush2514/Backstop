"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useScrambleLines } from "@/components/ui/text-randomized";
import type { CodeTab } from "@/lib/code";
import { IsoTerminal } from "@/components/iso-icons";
import { cn } from "@/lib/utils";
import { RevealSection, SectionHeading, ease } from "./ui";
import { siPython, siNodedotjs, siGo, siDjango, siPrisma } from "simple-icons";

const LANG_ICONS: Record<string, string> = {
  python: siPython.path,
  node:   siNodedotjs.path,
  go:     siGo.path,
  django: siDjango.path,
  prisma: siPrisma.path,
};

const CODE_LINE_STYLE: React.CSSProperties = {
  display: "block",
  fontFamily: "var(--font-geist-mono), ui-monospace, monospace",
  fontSize: 13,
  lineHeight: 1.55,
  counterReset: "line",
};

function ScrambleOverlay({ source, onDone }: { source: string; onDone: () => void }) {
  const reduced = useReducedMotion();
  const { lines, done } = useScrambleLines(source, !reduced);
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  useEffect(() => {
    if (done) onDoneRef.current();
  }, [done]);

  return (
    <pre style={{ margin: 0, padding: "20px 0", overflow: "hidden", background: "transparent", color: "var(--text-tertiary)" }}>
      <code style={CODE_LINE_STYLE}>
        {lines.map((line, i) => (
          <span key={i} className="line">{line}</span>
        ))}
      </code>
    </pre>
  );
}

const FLOW_NODES = [
  { id: "recv",  label: "Query received",     meta: "agent-gpt4 → production-db",     time: "0ms",  status: "neutral"  },
  { id: "ast",   label: "AST parse",          meta: "DROP TABLE users — CRITICAL",    time: "1ms",  status: "critical" },
  { id: "snap",  label: "Snapshot captured",  meta: "1,842,933 rows → s3://prod-snaps/", time: "2ms", status: "success" },
  { id: "exec",  label: "Query executes",     meta: "allowed after snapshot",         time: "3ms",  status: "neutral"  },
  { id: "log",   label: "Event logged",       meta: "audit trail updated",            time: "4ms",  status: "done"     },
] as const;

type FlowStatus = (typeof FLOW_NODES)[number]["status"];

export function FlowDot({ status, active, done }: { status: FlowStatus; active: boolean; done: boolean }) {
  return (
    <div className={cn(
      "relative z-10 mt-[3px] h-3 w-3 shrink-0 rounded-full border-2 transition-all duration-300",
      !done  && "border-[var(--border)] bg-[var(--bg-primary)]",
      done && status === "critical" && "border-[var(--danger)] bg-[rgba(255,68,68,0.15)]",
      done && status === "success"  && "border-[var(--success)] bg-[rgba(40,167,69,0.15)]",
      done && (status === "neutral" || status === "done") && "border-[var(--text-tertiary)] bg-[var(--bg-elevated)]",
    )}>
      {active && status === "critical" && (
        <span className="absolute -inset-1 rounded-full animate-ping bg-[rgba(255,68,68,0.3)]" />
      )}
      {done && status === "success" && (
        <svg className="absolute inset-0 m-auto h-1.5 w-1.5" viewBox="0 0 6 6" fill="none" aria-hidden="true">
          <polyline points="0.8,3 2.5,4.8 5.2,1.2" stroke="var(--success)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )}
    </div>
  );
}

export function ExecutionFlow() {
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { once: true, amount: 0.25 });
  const reduced = useReducedMotion();
  const [step, setStep] = useState(-1);

  useEffect(() => {
    if (!inView) return;
    if (reduced) { setStep(FLOW_NODES.length - 1); return; }
    FLOW_NODES.forEach((_, i) => setTimeout(() => setStep(i), 350 + i * 500));
  }, [inView, reduced]);

  const lineScale = step < 0 ? 0 : Math.min(1, (step + 1) / FLOW_NODES.length);
  const complete = step >= FLOW_NODES.length - 1;

  return (
    <div ref={ref} className="flex h-full flex-col rounded-xl border border-border bg-bg-secondary p-6">
      {/* Header */}
      <div className="mb-5 flex items-center gap-2">
        <IsoTerminal className="h-4 w-4 shrink-0" />
        <span className="font-mono text-[11px] uppercase tracking-[0.14em] text-text-tertiary">execution flow</span>
        <motion.span
          className={cn(
            "ml-auto rounded px-1.5 py-0.5 font-mono text-[10px] transition-colors duration-500",
            complete ? "bg-[rgba(40,167,69,0.12)] text-success" : "bg-bg-elevated text-text-tertiary"
          )}
          animate={complete ? { scale: [1, 1.08, 1] } : {}}
          transition={{ duration: 0.4 }}
        >
          {step < 0 ? "pending" : complete ? "complete" : `${step + 1} / ${FLOW_NODES.length}`}
        </motion.span>
      </div>

      {/* Pipeline */}
      <div className="relative flex flex-1 flex-col">
        {/* Track */}
        <div className="absolute left-[5px] top-2 bottom-2 w-0.5 rounded-full bg-[var(--border)]" />
        {/* Animated fill */}
        <motion.div
          className="absolute left-[5px] top-2 bottom-2 w-0.5 origin-top rounded-full"
          style={{
            background: "linear-gradient(to bottom, var(--danger), rgba(255,68,68,0.3))"
          }}
          animate={{ scaleY: lineScale }}
          initial={{ scaleY: 0 }}
          transition={{ duration: 0.38, ease: [0.16, 1, 0.3, 1] }}
        />

        <div className="flex h-full flex-col justify-between">
          {FLOW_NODES.map((node, i) => {
            const done = step >= i;
            const active = step === i;
            return (
              <motion.div
                key={node.id}
                className={cn(
                  "relative flex gap-3.5 transition-opacity duration-300",
                  done ? "opacity-100" : "opacity-25"
                )}
              >
                <FlowDot status={node.status} active={active} done={done} />

                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline justify-between gap-2">
                    <motion.span
                      className={cn(
                        "text-[13px] font-medium leading-snug transition-colors duration-200",
                        !done && "text-text-tertiary",
                        done && node.status === "critical" && "text-danger",
                        done && node.status === "success" && "text-success",
                        done && (node.status === "neutral" || node.status === "done") && "text-text-primary",
                      )}
                      animate={active ? { opacity: [0.5, 1, 0.7, 1] } : {}}
                      transition={{ duration: 0.55, times: [0, 0.3, 0.6, 1] }}
                    >
                      {node.label}
                    </motion.span>
                    <span className={cn(
                      "shrink-0 font-mono text-[10px] text-text-tertiary transition-opacity duration-300",
                      done ? "opacity-100" : "opacity-0"
                    )}>
                      {node.time}
                    </span>
                  </div>

                  <AnimatePresence>
                    {done && (
                      <motion.p
                        key={`meta-${node.id}`}
                        initial={{ opacity: 0, height: 0, y: -2 }}
                        animate={{ opacity: 1, height: "auto", y: 0 }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.22, delay: 0.1 }}
                        className={cn(
                          "mt-0.5 overflow-hidden text-[11px] leading-5",
                          node.status === "critical" ? "text-[rgba(255,68,68,0.6)]" :
                          node.status === "success"  ? "text-[rgba(40,167,69,0.7)]" :
                          "text-text-tertiary"
                        )}
                      >
                        {node.meta}
                      </motion.p>
                    )}
                  </AnimatePresence>
                </div>
              </motion.div>
            );
          })}
        </div>
      </div>

      {/* Footer hint */}
      <AnimatePresence>
        {complete && (
          <motion.div
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}
            className="mt-5 flex items-center gap-2 border-t border-border pt-4 font-mono text-[10px] text-text-tertiary"
          >
            <span className="h-1.5 w-1.5 rounded-full bg-success shadow-[0_0_6px_rgba(40,167,69,0.8)]" />
            <span>4ms total · 0 data lost · snapshot verified</span>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export function CodeIntegration({ codeTabs }: { codeTabs: CodeTab[] }) {
  const [active, setActive] = useState(codeTabs[0]?.id ?? "python");
  const [copied, setCopied] = useState(false);
  const [animKey, setAnimKey] = useState(0);
  const [scrambleSource, setScrambleSource] = useState<string | null>(null);
  const current = codeTabs.find((tab) => tab.id === active) ?? codeTabs[0];
  const reduced = useReducedMotion();

  function switchTab(id: string) {
    if (id === active) return;
    if (!reduced) {
      const newTab = codeTabs.find(t => t.id === id) ?? codeTabs[0];
      setScrambleSource(newTab?.source ?? null);
      setAnimKey(k => k + 1);
    }
    setActive(id);
  }

  async function copy() {
    if (!current) return;
    await navigator.clipboard.writeText(current.source);
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  }

  return (
    <RevealSection id="docs" className="border-y border-border bg-[rgba(255,68,68,0.03)] py-24">
      <SectionHeading before="Two lines. That's" red="it." />
      <div className="mx-auto mt-12 grid max-w-7xl gap-8 px-4 sm:px-6 lg:grid-cols-[1.05fr_0.95fr] lg:items-stretch lg:px-8">
        <div className="flex flex-col overflow-hidden rounded-xl border border-border bg-bg-secondary">
          {/* Tab bar */}
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <div className="flex gap-1 overflow-x-auto">
              {codeTabs.map((tab) => (
                <button
                  key={tab.id}
                  onClick={() => switchTab(tab.id)}
                  className="relative flex items-center gap-1.5 px-3 py-2 text-sm text-text-secondary transition hover:text-text-primary"
                  aria-label={`Show ${tab.label} code`}
                >
                  {active === tab.id && (
                    <motion.span layoutId="tab-indicator" className="absolute inset-x-2 bottom-0 h-0.5 bg-danger" />
                  )}
                  {LANG_ICONS[tab.id] && (
                    <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor" aria-hidden="true" className="shrink-0 opacity-70">
                      <path d={LANG_ICONS[tab.id]} />
                    </svg>
                  )}
                  <span className={cn(active === tab.id && "text-text-primary")}>{tab.label}</span>
                </button>
              ))}
            </div>
            <button onClick={copy} className="shrink-0 rounded-md p-2 text-text-secondary transition hover:bg-bg-elevated hover:text-text-primary" aria-label="Copy code">
              <AnimatePresence mode="wait" initial={false}>
                {copied ? (
                  <motion.span key="check" initial={{ opacity: 0, scale: 0.8 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0 }}>
                    <IconCheck size={18} stroke={1.5} className="text-success" />
                  </motion.span>
                ) : (
                  <motion.span key="copy" initial={{ opacity: 0, scale: 0.8 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0 }}>
                    <IconCopy size={18} stroke={1.5} />
                  </motion.span>
                )}
              </AnimatePresence>
            </button>
          </div>

          {/* Code pane — single .code-html so only one CSS counter ever renders */}
          <div className="code-html">
            {scrambleSource !== null ? (
              <ScrambleOverlay
                key={animKey}
                source={scrambleSource}
                onDone={() => setScrambleSource(null)}
              />
            ) : (
              current && <div dangerouslySetInnerHTML={{ __html: current.html }} />
            )}
          </div>
        </div>
        <ExecutionFlow />
      </div>
    </RevealSection>
  );
}
