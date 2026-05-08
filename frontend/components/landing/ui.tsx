"use client";

import { type MouseEvent as ReactMouseEvent, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useMotionValue, useReducedMotion, useSpring, useTransform } from "framer-motion";
import { cn } from "@/lib/utils";

export const ease = [0.16, 1, 0.3, 1] as const;

export function DeferredSection({
  children,
  className,
  minHeight = 600,
}: {
  children: React.ReactNode;
  className?: string;
  minHeight?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const visible = useInView(ref, { once: true, margin: "500px" } as Parameters<typeof useInView>[1]);
  return (
    <div ref={ref} className={className}>
      {visible ? children : <div style={{ minHeight }} />}
    </div>
  );
}

export function SectionHeading({ before, red, align = "center" }: { before: string; red: string; align?: "left" | "center" }) {
  return (
    <h2 style={{ textWrap: "balance" } as React.CSSProperties} className={cn("section-headline mx-auto max-w-4xl", align === "center" ? "px-4 text-center" : "text-left")}>
      {before} <span className="red-italic">{red}</span>
    </h2>
  );
}

export function BlurWords({ text, className, delay = 0 }: { text: string; className?: string; delay?: number }) {
  const reduced = useReducedMotion();
  const words = text.split(" ");
  const container = {
    hidden: {},
    visible: { transition: { staggerChildren: 0.08, delayChildren: delay } }
  };
  const wordVariants = {
    hidden: reduced ? { opacity: 1, filter: "blur(0px)", y: 0 } : { opacity: 0, filter: "blur(12px)", y: 6 },
    visible: { opacity: 1, filter: "blur(0px)", y: 0, transition: { duration: 0.55, ease: [0.16, 1, 0.3, 1] as [number, number, number, number] } }
  };
  return (
    <motion.span
      className={cn("inline", className)}
      variants={container}
      initial="hidden"
      whileInView="visible"
      viewport={{ once: true, amount: 0 }}
    >
      {words.map((w, i) => (
        <motion.span key={i} variants={wordVariants} style={{ display: "inline-block", marginRight: "0.22em" }}>
          {w}
        </motion.span>
      ))}
    </motion.span>
  );
}

export function RevealSection({ children, className, id }: { children: React.ReactNode; className?: string; id?: string }) {
  const ref = useRef<HTMLElement>(null);
  const inView = useInView(ref, { once: true, amount: 0.15 });
  const reduced = useReducedMotion();
  return (
    <motion.section
      id={id}
      ref={ref}
      initial={reduced ? { opacity: 1 } : { opacity: 0, y: 20 }}
      animate={inView ? { opacity: 1, y: 0 } : undefined}
      transition={{ duration: 0.45, ease }}
      className={className}
    >
      {children}
    </motion.section>
  );
}

export function SpotlightCard({ children, className }: { children: React.ReactNode; className?: string }) {
  function move(event: React.MouseEvent<HTMLDivElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    event.currentTarget.style.setProperty("--mouse-x", `${event.clientX - rect.left}px`);
    event.currentTarget.style.setProperty("--mouse-y", `${event.clientY - rect.top}px`);
  }
  return <div onMouseMove={move} className={cn("spotlight-card", className)}>{children}</div>;
}

export function RiskBadge({ risk }: { risk: string }) {
  const classes: Record<string, string> = {
    SAFE: "border-[rgba(40,167,69,0.55)] text-success",
    LOW: "border-[rgba(240,160,48,0.6)] text-warning",
    HIGH: "risk-high border-[rgba(240,160,48,0.8)] text-warning",
    CRITICAL: "risk-critical border-[rgba(255,68,68,0.8)] text-danger"
  };
  return <span className={cn("rounded-full border px-2 py-0.5 text-[11px] font-semibold", classes[risk])}>{risk}</span>;
}

export function AgentDot({ name }: { name: string }) {
  const palette = ["#FF4444", "#F0A030", "#28A745", "#8B949E"];
  const index = name.split("").reduce((sum, char) => sum + char.charCodeAt(0), 0) % palette.length;
  return (
    <span
      className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-border bg-bg-secondary text-[9px] text-text-primary"
      style={{ boxShadow: `inset 0 0 18px ${palette[index]}33` }}
      aria-hidden="true"
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  );
}

export function StatPill({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-bg-secondary px-3 py-2 font-mono text-xs text-text-secondary">
      <span className="text-[#f1e05a]">{icon}</span>
      {label}
    </div>
  );
}

export function GlowButton({
  href,
  children,
  compact,
  large,
  className
}: {
  href: string;
  children: React.ReactNode;
  compact?: boolean;
  large?: boolean;
  className?: string;
}) {
  return (
    <motion.a
      href={href}
      whileHover={{ scale: large ? 1.02 : 1.01, boxShadow: large ? "0 0 20px rgba(255,68,68,0.18)" : "0 0 10px rgba(255,68,68,0.14)" }}
      whileTap={{ scale: 0.98 }}
      transition={{ type: "spring", stiffness: 400, damping: 17 }}
      className={cn(
        "inline-flex items-center gap-2 rounded-md bg-danger font-semibold text-white",
        compact ? "px-3 py-2 text-sm" : large ? "px-8 py-4 text-lg" : "px-5 py-3",
        className
      )}
    >
      {children}
    </motion.a>
  );
}

export function GhostButton({ href, children, target, rel }: { href: string; children: React.ReactNode; target?: string; rel?: string }) {
  return (
    <motion.a
      href={href}
      target={target}
      rel={rel}
      whileHover="hover"
      className="inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md border border-border px-5 py-3 text-text-secondary transition hover:bg-bg-secondary hover:text-text-primary"
    >
      {children}
    </motion.a>
  );
}

const WOBBLE_SPRING = { stiffness: 400, damping: 35 };

export function WobbleCard({
  children,
  containerClassName,
  cardBg,
  onEnter,
  onLeave,
}: {
  children: React.ReactNode;
  containerClassName?: string;
  cardBg?: string;
  onEnter?: () => void;
  onLeave?: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const reduced = useReducedMotion();
  const mouseX = useMotionValue(0);
  const mouseY = useMotionValue(0);
  const rotateX = useSpring(useTransform(mouseY, [-0.5, 0.5], [6, -6]), WOBBLE_SPRING);
  const rotateY = useSpring(useTransform(mouseX, [-0.5, 0.5], [-6, 6]), WOBBLE_SPRING);

  function onMouseMove(e: ReactMouseEvent<HTMLDivElement>) {
    if (!ref.current) return;
    const rect = ref.current.getBoundingClientRect();
    mouseX.set((e.clientX - rect.left) / rect.width - 0.5);
    mouseY.set((e.clientY - rect.top) / rect.height - 0.5);
    ref.current.style.setProperty("--mouse-x", `${e.clientX - rect.left}px`);
    ref.current.style.setProperty("--mouse-y", `${e.clientY - rect.top}px`);
    if (cardBg) ref.current.style.setProperty("--card-bg", cardBg);
  }

  function onMouseLeave() {
    mouseX.set(0);
    mouseY.set(0);
    onLeave?.();
  }

  return (
    <motion.div
      ref={ref}
      onMouseMove={onMouseMove}
      onMouseEnter={onEnter}
      onMouseLeave={onMouseLeave}
      style={{
        ...(reduced ? {} : { rotateX, rotateY, transformStyle: "preserve-3d" as const }),
        ...(cardBg ? ({ "--card-bg": cardBg } as Record<string, string>) : {}),
      }}
      className={cn("spotlight-card border-beam relative overflow-hidden rounded-xl border border-border", containerClassName)}
    >
      {children}
    </motion.div>
  );
}
