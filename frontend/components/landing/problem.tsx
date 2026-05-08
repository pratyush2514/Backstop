"use client";

import { motion } from "framer-motion";
import { IconBolt, IconClockExclamation, IconKey } from "@tabler/icons-react";
import { cn } from "@/lib/utils";
import { RevealSection, SectionHeading, SpotlightCard } from "./ui";

const ease = [0.16, 1, 0.3, 1] as const;

function ProblemVisual({ kind }: { kind: string }) {
  if (kind === "credentials") {
    return (
      <svg className="absolute right-4 top-4 h-28 w-36 opacity-[0.16]" viewBox="0 0 160 120" fill="none" aria-hidden="true">
        <path d="M32 84C32 72.954 40.954 64 52 64H108C119.046 64 128 72.954 128 84V98H32V84Z" stroke="#FF4444" strokeWidth="2" />
        <path d="M56 64V48C56 34.745 66.745 24 80 24C93.255 24 104 34.745 104 48V64" stroke="#E6EDF3" strokeWidth="2" />
        <path d="M80 80V92" stroke="#FF4444" strokeWidth="2" />
        <circle cx="80" cy="76" r="5" stroke="#FF4444" strokeWidth="2" />
      </svg>
    );
  }
  if (kind === "agent") {
    return (
      <svg className="absolute right-4 top-4 h-28 w-36 opacity-[0.15]" viewBox="0 0 160 120" fill="none" aria-hidden="true">
        <rect x="42" y="36" width="76" height="54" rx="10" stroke="#F0A030" strokeWidth="2" />
        <path d="M80 36V20" stroke="#F0A030" strokeWidth="2" />
        <circle cx="80" cy="18" r="5" stroke="#F0A030" strokeWidth="2" />
        <circle cx="66" cy="60" r="4" fill="#E6EDF3" />
        <circle cx="94" cy="60" r="4" fill="#E6EDF3" />
        <path d="M66 76H94" stroke="#FF4444" strokeWidth="2" />
        <path d="M24 92H136" stroke="#21262D" strokeWidth="2" />
      </svg>
    );
  }
  return (
    <svg className="absolute right-4 top-4 h-28 w-36 opacity-[0.16]" viewBox="0 0 160 120" fill="none" aria-hidden="true">
      <circle cx="80" cy="62" r="36" stroke="#FF4444" strokeWidth="2" />
      <path d="M80 40V64L98 74" stroke="#E6EDF3" strokeWidth="2" strokeLinecap="round" />
      <path d="M46 22L32 34M114 22L128 34" stroke="#FF4444" strokeWidth="2" />
      <path d="M38 98L54 86M122 98L106 86" stroke="#21262D" strokeWidth="2" />
    </svg>
  );
}

export function ProblemSection() {
  const cards = [
    {
      title: "They have your credentials",
      body: "You gave the agent DATABASE_URL. It can see everything, change everything, delete everything. Without asking.",
      icon: IconKey,
      color: "text-danger",
      visual: "credentials"
    },
    {
      title: "They don't understand consequences",
      body: "An LLM doesn't know the difference between a test database and production. It executes what it plans.",
      icon: IconBolt,
      color: "text-warning",
      visual: "agent"
    },
    {
      title: "It happens in seconds",
      body: "PocketOS: 9 seconds. By the time you see the Slack alert, the data is gone.",
      icon: IconClockExclamation,
      color: "text-danger",
      visual: "clock"
    }
  ];

  return (
    <RevealSection className="py-24">
      <SectionHeading before="AI agents don't ask" red="permission." />
      <div className="mx-auto mt-12 grid max-w-7xl gap-4 px-4 sm:px-6 md:grid-cols-3 lg:px-8">
        {cards.map((card, i) => (
          <motion.div
            key={card.title}
            initial={{ opacity: 0, y: 24, scale: 0.97 }}
            whileInView={{ opacity: 1, y: 0, scale: 1 }}
            viewport={{ once: true, amount: 0.15 }}
            transition={{ duration: 0.5, ease, delay: i * 0.1 }}
          >
            <SpotlightCard className="border-beam relative min-h-[260px] overflow-hidden rounded-xl border border-border p-6">
              <ProblemVisual kind={card.visual} />
              <div className={cn("relative mb-8 flex h-11 w-11 items-center justify-center rounded-md bg-[rgba(255,68,68,0.08)]", card.color)}>
                <card.icon size={24} stroke={1.5} />
              </div>
              <h3 className="relative text-xl font-semibold">{card.title}</h3>
              <p className="relative mt-4 leading-7 text-text-secondary">{card.body}</p>
            </SpotlightCard>
          </motion.div>
        ))}
      </div>
    </RevealSection>
  );
}
