"use client";

import Link from "next/link";
import { IconBrandGithub, IconBrandX, IconBrandDiscord, IconExternalLink, IconShieldCheck } from "@tabler/icons-react";
import { LogoMark } from "./nav";

const SOCIAL = [
  { icon: IconBrandGithub,  label: "GitHub",  href: "https://github.com/pratyush2514/Backstop" },
  { icon: IconBrandX,       label: "Project updates", href: "https://twitter.com/backstop_dev" },
  { icon: IconBrandDiscord, label: "Incident docs", href: "/docs/operations/runbooks" },
];

const COLUMNS: {
  title: string;
  links: { label: string; href: string; external?: boolean; soon?: boolean; live?: boolean; operational?: boolean }[];
}[] = [
  {
    title: "PRODUCT",
    links: [
      { label: "Docs",        href: "/docs" },
      { label: "Architecture",   href: "/docs/getting-started/how-it-works" },
      { label: "Pricing",     href: "#pricing" },
      { label: "MCP Server",   href: "/docs/mcp/overview" },
      { label: "Open Source", href: "https://github.com/pratyush2514/Backstop", external: true },
    ],
  },
  {
    title: "RESOURCES",
    links: [
      { label: "Quick Start",            href: "/docs/getting-started/quick-start" },
      { label: "Runbooks",       href: "/docs/operations/runbooks", live: true },
      { label: "GitHub",          href: "https://github.com/pratyush2514/Backstop", external: true },
      { label: "PocketOS Story",  href: "https://www.theregister.com/2026/04/27/cursoropus_agent_snuffs_out_pocketos/", external: true },
      { label: "Production checklist",          href: "/docs/operations/production-checklist", operational: true },
    ],
  },
  {
    title: "LEGAL",
    links: [
      { label: "License",    href: "https://github.com/pratyush2514/Backstop/blob/main/LICENSE", external: true },
      { label: "Security boundaries",  href: "/docs/security/security-boundaries" },
      { label: "Credential model",          href: "/docs/security/credential-model" },
      { label: "Token scopes",             href: "/docs/security/token-scopes" },
    ],
  },
];

export function Footer() {
  return (
    <footer className="relative overflow-hidden bg-[#040608]">
      {/* Bottom-center red glow */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute bottom-0 left-1/2 h-[320px] w-[700px] -translate-x-1/2"
        style={{ background: "radial-gradient(ellipse at center, rgba(255,68,68,0.07) 0%, transparent 70%)" }}
      />

      <div className="relative mx-auto max-w-7xl px-4 pt-16 sm:px-6 lg:px-8">

        {/* ── Main grid ──────────────────────────────────────────────── */}
        <div className="grid gap-12 lg:grid-cols-[1fr_2fr]">

          {/* Brand column */}
          <div>
            <Link href="/" className="inline-flex items-center gap-2.5" aria-label="Backstop home">
              <LogoMark />
              <span
                style={{ fontFamily: "var(--font-brand)", fontWeight: 700, fontSize: "18px", letterSpacing: "-0.035em" }}
                className="text-text-primary"
              >
                Backstop
              </span>
            </Link>

            <p className="mt-5 max-w-[260px] text-sm leading-relaxed text-text-secondary">
              The last line of defense between your AI agent and production.
            </p>

            <ul className="mt-8 flex gap-2.5">
              {SOCIAL.map(({ icon: Icon, label, href }) => (
                <li key={label}>
                  <a
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label={label}
                    className="flex h-9 w-9 items-center justify-center rounded-lg border border-border text-text-secondary transition-all duration-200 hover:border-[rgba(255,68,68,0.45)] hover:bg-[rgba(255,68,68,0.06)] hover:text-danger"
                  >
                    <Icon size={15} stroke={1.5} />
                  </a>
                </li>
              ))}
            </ul>
          </div>

          {/* Link columns */}
          <div className="grid grid-cols-2 gap-8 sm:grid-cols-3">
            {COLUMNS.map(({ title, links }) => (
              <div key={title}>
                <p className="font-mono text-[10px] font-bold tracking-[0.14em] text-text-tertiary">
                  {title}
                </p>
                <ul className="mt-6 space-y-3.5">
                  {links.map(({ label, href, external, soon, live, operational }) => (
                    <li key={label}>
                      <a
                        href={href}
                        target={external ? "_blank" : undefined}
                        rel={external ? "noopener noreferrer" : undefined}
                        className="group inline-flex items-center gap-1.5 text-sm text-text-secondary transition-colors duration-150 hover:text-text-primary"
                      >
                        {operational && (
                          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success shadow-[0_0_6px_rgba(40,167,69,0.9)]" />
                        )}
                        {live && (
                          <span className="relative flex h-2 w-2 shrink-0">
                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-danger opacity-50" />
                            <span className="relative inline-flex h-2 w-2 rounded-full bg-danger" />
                          </span>
                        )}
                        {label}
                        {external && (
                          <IconExternalLink size={11} className="shrink-0 opacity-0 transition-opacity group-hover:opacity-50" />
                        )}
                        {soon && (
                          <span className="rounded bg-bg-elevated px-1.5 py-0.5 font-mono text-[9px] tracking-wide text-text-tertiary">
                            soon
                          </span>
                        )}
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </div>

        {/* ── Bottom bar ─────────────────────────────────────────────── */}
        <div className="mt-14 flex flex-col gap-2 border-t border-border pt-5 text-xs text-text-tertiary sm:flex-row sm:items-center sm:justify-between">
          <span>© 2026 Backstop — Built in public. Apache-2.0 licensed.</span>
          <span className="flex items-center gap-1.5">
            <IconShieldCheck size={12} stroke={1.5} className="text-success" />
            Self-hosted by default
          </span>
        </div>

        {/* ── Watermark ──────────────────────────────────────────────── */}
        <div aria-hidden="true" className="pointer-events-none select-none overflow-hidden">
          <p
            style={{
              fontFamily: "var(--font-brand)",
              fontWeight: 700,
              fontSize: "clamp(5rem, 15vw, 11rem)",
              letterSpacing: "-0.04em",
              lineHeight: 0.9,
              color: "rgba(230,237,243,0.035)",
              transform: "translateY(22%)",
              whiteSpace: "nowrap",
            }}
          >
            Backstop
          </p>
        </div>

      </div>
    </footer>
  );
}
