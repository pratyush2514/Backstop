"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { IconBolt, IconBrandGithub, IconListCheck, IconRobot } from "@tabler/icons-react";
import { siGithub } from "simple-icons";
import { FloatIcon, IsoLock, IsoRefresh, IsoShield, LottieRefresh } from "@/components/iso-icons";
import { cn } from "@/lib/utils";
import { RevealSection, SectionHeading, WobbleCard } from "./ui";

const TAG_CLASS = "rounded-md bg-bg-elevated px-2 py-0.5 font-mono text-[11px] text-text-tertiary";
const BADGE_CLASS = "absolute right-4 top-4 rounded-full border border-border bg-bg-elevated px-2.5 py-1 text-[11px] font-medium tracking-wide text-text-secondary";
const WATERMARK_CLASS = "pointer-events-none absolute -bottom-4 -right-4 opacity-[0.05]";

export function BentoSection() {
  const [hovered, setHovered] = useState(false);

  return (
    <RevealSection className="py-24">
      <SectionHeading before="Everything you need. Nothing you" red="don't." />
      <div className="mx-auto mt-12 grid max-w-7xl gap-4 px-4 sm:px-6 md:grid-cols-3 lg:px-8">

        {/* Large Restore card */}
        <motion.div
          className="md:col-span-2 md:row-span-2"
          initial={{ opacity: 0, y: 28 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.1 }}
          transition={{ duration: 0.6, ease: [0.16, 1, 0.3, 1] }}
        >
        <WobbleCard
          containerClassName="p-6 h-full always-beam"
          cardBg="linear-gradient(135deg, rgba(255,68,68,0.18) 0%, #0d1117 58%)"
          onEnter={() => setHovered(true)}
          onLeave={() => setHovered(false)}
        >
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <IsoRefresh className={cn(WATERMARK_CLASS, "h-56 w-auto -bottom-8 -right-8")} />
          <div className="absolute right-4 top-4 rounded-full border border-[rgba(255,68,68,0.3)] bg-[rgba(255,68,68,0.08)] px-2.5 py-1 text-[11px] font-medium tracking-wide text-danger">
            Featured
          </div>
          <div className="relative z-10">
            <div className="flex items-start gap-4">
              <FloatIcon><IsoRefresh className="h-14 w-auto" /></FloatIcon>
              {hovered && <LottieRefresh className="-ml-2 h-14 w-14 opacity-80" />}
            </div>
            <h3 className="mt-6 text-2xl font-semibold">Fast Table Restore</h3>
            <p className="mt-4 max-w-xl leading-7 text-text-secondary">
              Restore preview first, then recover into a separate table. The full flow stays in your infrastructure and timing depends on table size, storage throughput, and validation steps.
            </p>
            <div className="mt-8 rounded-lg border border-border bg-bg-primary p-5 font-mono text-sm">
              <div className="mb-3 flex justify-between text-text-tertiary">
                <span>snap_a3f9</span>
                <span>{hovered ? "100%" : "0%"}</span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-bg-elevated">
                <motion.div className="h-full origin-left bg-success" animate={{ scaleX: hovered ? 1 : 0 }} transition={{ duration: 1.2 }} />
              </div>
              <div className="mt-4 text-success">
                {hovered ? "✓ Recovered table built from verified snapshot manifest" : <span className="text-xs text-text-tertiary">hover to replay →</span>}
              </div>
            </div>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#s3", "#preview", "#recovered-table", "#dry-run"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

        {/* BYOS card */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.15 }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: 0.1 }}
        >
        <WobbleCard containerClassName="p-6 h-full" cardBg="linear-gradient(135deg, rgba(0,102,255,0.16) 0%, #0d1117 58%)">
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <IsoShield className={cn(WATERMARK_CLASS, "h-32 w-auto")} />
          <span className={BADGE_CLASS}>BYOS</span>
          <div className="relative z-10">
            <div className="flex items-end gap-1">
              <FloatIcon><IsoShield className="h-10 w-auto" /></FloatIcon>
              <FloatIcon className="-mb-1"><IsoLock className="h-8 w-auto opacity-80" /></FloatIcon>
            </div>
            <h3 className="mt-6 text-lg font-semibold">Bring Your Own Storage</h3>
            <p className="mt-3 text-sm leading-6 text-text-secondary">Snapshots are written to AWS S3 or a compatible endpoint such as MinIO.</p>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#aws-s3", "#minio", "#byos"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

        {/* Open Source */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.15 }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: 0.2 }}
        >
        <WobbleCard containerClassName="p-6 h-full" cardBg="linear-gradient(135deg, rgba(40,167,69,0.15) 0%, #0d1117 58%)">
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <svg viewBox="0 0 24 24" fill="currentColor" className={cn(WATERMARK_CLASS, "h-32 w-auto text-current")} aria-hidden="true">
            <path d={siGithub.path} />
          </svg>
          <span className={BADGE_CLASS}>MIT</span>
          <div className="relative z-10">
            <IconBrandGithub size={34} stroke={1.5} className="text-danger opacity-75" />
            <h3 className="mt-4 text-lg font-semibold">Open Source Core</h3>
            <p className="mt-3 text-sm leading-6 text-text-secondary">Read the SDK, gateway, sidecar, restore engine, and launch drills.</p>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#open-source", "#auditable"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

        {/* Agent Identity */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.15 }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: 0.05 }}
        >
        <WobbleCard containerClassName="p-6 h-full" cardBg="linear-gradient(135deg, rgba(240,160,48,0.15) 0%, #0d1117 58%)">
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <IconRobot size={120} stroke={0.8} className={cn(WATERMARK_CLASS, "text-text-tertiary")} />
          <div className="relative z-10">
            <IconRobot size={34} stroke={1.5} className="text-danger opacity-75" />
            <h3 className="mt-4 text-lg font-semibold">Agent Identity Tracking</h3>
            <p className="mt-3 text-sm leading-6 text-text-secondary">Stable actor names connect SQL events to the agent that ran them.</p>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#attribution", "#actor-id"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

        {/* Audit Trail */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.15 }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: 0.15 }}
        >
        <WobbleCard containerClassName="p-6 h-full" cardBg="linear-gradient(135deg, rgba(139,92,246,0.15) 0%, #0d1117 58%)">
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <IconListCheck size={120} stroke={0.8} className={cn(WATERMARK_CLASS, "text-text-tertiary")} />
          <span className={BADGE_CLASS}>Durable</span>
          <div className="relative z-10">
            <IconListCheck size={34} stroke={1.5} className="text-danger opacity-75" />
            <h3 className="mt-4 text-lg font-semibold">Immutable Audit Trail</h3>
            <p className="mt-3 text-sm leading-6 text-text-secondary">Audit and snapshot records capture the table, operation, actor, row count, and storage references for review.</p>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#compliance", "#manifests"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

        {/* Parser Bench */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, amount: 0.15 }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: 0.25 }}
        >
        <WobbleCard containerClassName="p-6 h-full" cardBg="linear-gradient(135deg, rgba(6,182,212,0.15) 0%, #0d1117 58%)">
          <div className="dot-grid pointer-events-none absolute inset-0 opacity-50" />
          <IconBolt size={120} stroke={0.8} className={cn(WATERMARK_CLASS, "text-text-tertiary")} />
          <span className={cn(BADGE_CLASS, "font-mono")}>local</span>
          <div className="relative z-10">
            <IconBolt size={34} stroke={1.5} className="text-danger opacity-75" />
            <h3 className="mt-4 text-lg font-semibold">Parser Benchmark</h3>
            <p className="mt-3 text-sm leading-6 text-text-secondary">The CLI includes a local parser benchmark command so you can measure classifier overhead in your own environment.</p>
            <div className="mt-4 flex flex-wrap gap-1.5">
              {["#performance", "#ast"].map(t => <span key={t} className={TAG_CLASS}>{t}</span>)}
            </div>
          </div>
        </WobbleCard>
        </motion.div>

      </div>
    </RevealSection>
  );
}
