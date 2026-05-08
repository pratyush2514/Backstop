"use client";

import Image from "next/image";
import { IconArrowRight, IconBrandGithub, IconShieldCheck, IconStarFilled } from "@tabler/icons-react";
import type { GitHubRepoData } from "@/lib/github";
import { formatNumber } from "@/lib/utils";
import { FloatIcon, IsoTerminal } from "@/components/iso-icons";
import { GhostButton, GlowButton, RevealSection, SectionHeading, StatPill } from "./ui";

export function OpenSourceSection({ github }: { github: GitHubRepoData }) {
  return (
    <RevealSection id="open-source" className="border-y border-border bg-bg-secondary py-24">
      <div className="mx-auto grid max-w-7xl gap-10 px-4 sm:px-6 lg:grid-cols-[0.9fr_1.1fr] lg:px-8">
        <div>
          <SectionHeading before="Read every line. Trust what you" red="deploy." align="left" />
          <p className="mt-6 max-w-xl leading-8 text-text-secondary">
            Backstop's core is Apache-2.0 licensed. When a tool stands between an AI agent and your production data, you deserve to read the gateway, sidecar, MCP server, SDKs, restore path, and drills yourself.
          </p>
          <div className="mt-8 flex flex-col gap-3 sm:flex-row">
            <GlowButton href={github.url}>View on GitHub <IconArrowRight size={18} stroke={1.5} /></GlowButton>
            <GhostButton href="/docs">Read the docs <IconArrowRight size={18} stroke={1.5} /></GhostButton>
          </div>
        </div>
        <div className="rounded-xl border border-border bg-bg-primary p-6">
          <div className="mb-6 flex items-start justify-between">
            <div>
              <div className="font-mono text-sm text-text-secondary">{github.name}</div>
              <div className="mt-2 text-2xl font-semibold">Open source core</div>
            </div>
            <FloatIcon>
              <IsoTerminal className="h-14 w-auto" />
            </FloatIcon>
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <StatPill icon={<IconStarFilled size={16} />} label={github.stars === null ? "stars unavailable" : `${formatNumber(github.stars)} stars`} />
            <StatPill icon={<IconBrandGithub size={16} />} label={github.forks === null ? "forks unavailable" : `${formatNumber(github.forks)} forks`} />
            <StatPill icon={<IconShieldCheck size={16} />} label={github.license ?? "Apache-2.0"} />
          </div>
          <div className="mt-5 rounded-lg border border-border bg-bg-secondary p-4">
            <div className="text-xs uppercase tracking-[0.14em] text-text-tertiary">Last commit</div>
            <div className="mt-3 font-mono text-sm text-text-secondary">{github.lastCommitMessage}</div>
            {github.lastCommitRelative && <div className="mt-2 text-xs text-text-tertiary">{github.lastCommitRelative}</div>}
          </div>
          <div className="mt-5 flex -space-x-2">
            {github.contributors.length === 0 ? (
              <span className="text-sm text-text-tertiary">Contributor avatars load from GitHub when available.</span>
            ) : (
              github.contributors.map((person) => (
                <a key={person.login} href={person.url} aria-label={`GitHub contributor ${person.login}`}>
                  <Image src={person.avatarUrl} alt={person.login} width={36} height={36} className="rounded-full border border-bg-primary" />
                </a>
              ))
            )}
          </div>
        </div>
      </div>
    </RevealSection>
  );
}
