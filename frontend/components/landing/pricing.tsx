"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { IconArrowRight, IconCheck, IconMinus } from "@tabler/icons-react";
import { cn } from "@/lib/utils";
import { GhostButton, GlowButton, RevealSection, SectionHeading } from "./ui";

export function PricingSection() {
  const [planned, setPlanned] = useState(false);
  const cards = [
    ["OSS", "$0", "available today", ["Open source core", "Gateway + sidecar", "MCP + Node SDK", "CLI restore", "Doctor and drill commands"], []],
    ["Pro", planned ? "TBD" : "Soon", "planned", ["Commercial workflow layer", "Managed operator experience", "Notification integrations", "Longer retention options", "Hosted control plane"], ["Not shipping today"]],
    ["Team", planned ? "TBD" : "Soon", "planned", ["Org-level policy management", "Enterprise identity integrations", "Managed audit views", "Support and rollout help"], ["Not shipping today"]]
  ] as const;

  return (
    <RevealSection id="pricing" className="py-24">
      <SectionHeading before="Start free. Scale when you're" red="ready." />
      <div className="mx-auto mt-8 flex w-fit rounded-lg border border-border bg-bg-secondary p-1">
        {[
          ["Available now", false],
          ["Planned", true]
        ].map(([label, value]) => (
          <button key={label as string} onClick={() => setPlanned(Boolean(value))} className="relative px-4 py-2 text-sm text-text-secondary" aria-label={`Show ${label} pricing`}>
            {planned === Boolean(value) && <motion.span layoutId="pricing-toggle" className="absolute inset-0 rounded-md bg-danger" />}
            <span className={cn("relative", planned === Boolean(value) && "text-white")}>{label as string}</span>
          </button>
        ))}
      </div>
      <div className="mx-auto mt-12 grid max-w-7xl gap-4 px-4 sm:px-6 lg:grid-cols-3 lg:px-8">
        {cards.map(([name, price, unit, included, excluded], i) => (
          <motion.div
            key={name}
            initial={{ opacity: 0, y: 24 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true, amount: 0.15 }}
            transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1], delay: i * 0.12 }}
            className={cn(
              "relative rounded-xl border border-border bg-bg-secondary p-6",
              name === "Pro" && "border-[rgba(255,68,68,0.5)] shadow-[0_0_60px_rgba(255,68,68,0.08)] lg:-my-3 lg:py-9"
            )}
          >
            {name === "Pro" && <div className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 rounded-full bg-danger px-3 py-1 text-xs font-medium text-white">Most popular</div>}
            <h3 className="text-xl font-semibold">{name}</h3>
            <div className="mt-5 flex items-end gap-1">
              <span className="font-serif text-5xl">{price}</span>
              <span className="pb-2 text-text-secondary">{name === "OSS" ? "" : ""}</span>
            </div>
            <p className="mt-1 text-sm text-text-tertiary">{unit}</p>
            <div className="mt-6 space-y-3">
              {[...included, ...excluded].map((feature) => {
                const missing = excluded.includes(feature as never);
                return (
                  <div key={feature} className="flex items-center gap-3 text-sm text-text-secondary">
                    {missing ? <IconMinus size={16} stroke={1.5} className="text-text-tertiary" /> : <IconCheck size={16} stroke={1.5} className="text-success" />}
                    <span>{feature}</span>
                  </div>
                );
              })}
            </div>
            <GlowButton href="#cta" className="mt-7 w-full justify-center" compact>
              {name === "OSS" ? "Use OSS" : name === "Pro" ? "Follow roadmap" : "Talk to us"}
            </GlowButton>
          </motion.div>
        ))}
      </div>
      <div className="mx-auto mt-6 max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col gap-4 rounded-xl border border-dashed border-border bg-gradient-to-br from-bg-secondary to-bg-primary p-6 md:flex-row md:items-center md:justify-between">
          <div>
            <h3 className="text-lg font-semibold">Commercial workflow layer is still being shaped.</h3>
            <p className="mt-2 text-sm text-text-secondary">The self-hosted OSS core is the product available today. Managed and enterprise workflows are roadmap discussions, not shipped features.</p>
          </div>
          <GhostButton href="#contact">Talk to us <IconArrowRight size={18} stroke={1.5} /></GhostButton>
        </div>
      </div>
    </RevealSection>
  );
}
