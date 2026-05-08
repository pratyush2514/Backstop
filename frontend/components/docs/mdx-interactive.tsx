"use client";

import { type ReactNode, type HTMLAttributes, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { IconCopy, IconCheck } from "@tabler/icons-react";
import { motion, AnimatePresence } from "framer-motion";

/* ─── Code block — Premium Terminal Style ───────────────────────── */

export function Pre({ children, ...props }: HTMLAttributes<HTMLPreElement>) {
  const [copied, setCopied] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);
  const lang = (props as { "data-language"?: string })["data-language"];
  const title = (props as { "data-title"?: string })["data-title"];

  function copy() {
    const text = preRef.current?.querySelector("code")?.textContent ?? "";
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <motion.div 
      initial={{ opacity: 0, y: 10 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-50px" }}
      transition={{ duration: 0.4, ease: "easeOut" }}
      className="relative group rounded-xl border border-white/10 bg-[#09090b] my-8 shadow-xl overflow-hidden"
    >
      <button
        onClick={copy}
        className="absolute top-4 right-4 p-1.5 rounded-md text-white/40 hover:text-white hover:bg-white/10 transition-all opacity-0 group-hover:opacity-100 z-10"
        aria-label="Copy code"
      >
        {copied ? <IconCheck size={16} stroke={2} className="text-green-400" /> : <IconCopy size={16} stroke={1.5} />}
      </button>
      <div className="relative">
        <div className="absolute top-0 right-0 w-32 h-full bg-gradient-to-l from-[#09090b] to-transparent pointer-events-none z-0" />
        <pre ref={preRef} {...props} className={cn("p-5 pt-5 overflow-x-auto text-[14px] leading-loose font-mono text-white/80 relative z-0", props.className)}>
          {children}
        </pre>
      </div>
    </motion.div>
  );
}

/* ─── Animated Tabs ────────────────────────────────────────────── */

export function Tabs({ items, children }: { items: string[]; children: ReactNode[] }) {
  const [active, setActive] = useState(0);

  return (
    <motion.div 
      initial={{ opacity: 0, y: 10 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true }}
      className="my-8 rounded-xl border border-white/10 bg-[#0c0c0e] shadow-xl overflow-hidden"
    >
      <div className="flex gap-2 p-2 border-b border-white/5 bg-white/[0.02] overflow-x-auto relative">
        {items.map((label, i) => (
          <button
            key={i}
            role="tab"
            aria-selected={active === i}
            onClick={() => setActive(i)}
            className={cn(
              "relative px-4 py-2 text-[14px] font-medium rounded-lg transition-colors whitespace-nowrap outline-none",
              active === i ? "text-white" : "text-white/50 hover:text-white/80 hover:bg-white/5"
            )}
          >
            {active === i && (
              <motion.div
                layoutId="tab-indicator"
                className="absolute inset-0 rounded-lg bg-white/10 border border-white/5"
                initial={false}
                transition={{ type: "spring", bounce: 0.15, duration: 0.5 }}
              />
            )}
            <span className="relative z-10">{label}</span>
          </button>
        ))}
      </div>
      <div className="p-1">
        <AnimatePresence mode="wait">
          <motion.div
            key={active}
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -4 }}
            transition={{ duration: 0.15 }}
          >
            {Array.isArray(children) ? children[active] : children}
          </motion.div>
        </AnimatePresence>
      </div>
    </motion.div>
  );
}

export function Tab({ children }: { children: ReactNode }) {
  return <div>{children}</div>;
}
