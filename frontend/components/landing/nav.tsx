"use client";

import { useEffect, useState } from "react";
import { motion, useScroll, useSpring } from "framer-motion";
import { IconArrowRight, IconBrandGithub } from "@tabler/icons-react";
import type { GitHubRepoData } from "@/lib/github";
import { formatNumber } from "@/lib/utils";
import { GlowButton } from "./ui";

export function LogoMark() {
  return (
    <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
      <path d="M14 3L23 6.7V13.8C23 19.3 19.2 23.5 14 25C8.8 23.5 5 19.3 5 13.8V6.7L14 3Z" stroke="#FF4444" strokeWidth="1.6" />
      <path d="M10 10.2C10 8.9 11.8 7.9 14 7.9C16.2 7.9 18 8.9 18 10.2C18 11.5 16.2 12.5 14 12.5C11.8 12.5 10 11.5 10 10.2Z" stroke="#E6EDF3" strokeWidth="1.3" />
      <path d="M10 10.4V16.4C10 17.7 11.8 18.7 14 18.7C16.2 18.7 18 17.7 18 16.4V10.4" stroke="#E6EDF3" strokeWidth="1.3" />
      <path d="M7.5 20.3L20.8 6.9" stroke="#FF4444" strokeWidth="1.2" />
    </svg>
  );
}

export function ScrollProgress() {
  const { scrollYProgress } = useScroll();
  const scaleX = useSpring(scrollYProgress, { stiffness: 100, damping: 30, restDelta: 0.001 });
  return <motion.div className="fixed left-0 top-0 z-50 h-[2px] w-full origin-left bg-danger" style={{ scaleX }} />;
}

export function CustomCursor() {
  const [position, setPosition] = useState({ x: -100, y: -100 });
  const [active, setActive] = useState(false);

  useEffect(() => {
    const finePointer = window.matchMedia("(pointer: fine)").matches;
    if (!finePointer) return;

    const onMove = (event: MouseEvent) => setPosition({ x: event.clientX, y: event.clientY });
    const onOver = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      setActive(Boolean(target?.closest("a,button,.code-html")));
    };

    window.addEventListener("mousemove", onMove, { passive: true });
    window.addEventListener("mouseover", onOver, { passive: true });
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseover", onOver);
    };
  }, []);

  return (
    <motion.div
      aria-hidden="true"
      className="pointer-events-none fixed left-0 top-0 z-[60] hidden h-5 w-5 rounded-full border border-[rgba(255,68,68,0.65)] mix-blend-screen md:block"
      animate={{
        x: position.x - 10,
        y: position.y - 10,
        scale: active ? 1.65 : 1,
        backgroundColor: active ? "rgba(255,68,68,0.12)" : "rgba(255,68,68,0)"
      }}
      transition={{ type: "spring", stiffness: 500, damping: 32, mass: 0.4 }}
    />
  );
}

export function Navigation({ github }: { github: GitHubRepoData }) {
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 48);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <div className="fixed inset-x-0 top-0 z-40 flex justify-center" suppressHydrationWarning>
      <motion.header
        className="w-full overflow-hidden"
        suppressHydrationWarning
        initial={false}
        animate={
          scrolled
            ? {
                maxWidth: 820,
                marginTop: 12,
                marginLeft: 16,
                marginRight: 16,
                borderRadius: 9999,
                backgroundColor: "rgba(8,12,16,0.92)",
                boxShadow: "0 4px 32px rgba(0,0,0,0.55), 0 0 0 1px rgba(255,255,255,0.07)",
                backdropFilter: "blur(20px)",
              }
            : {
                maxWidth: 10000,
                marginTop: 0,
                marginLeft: 0,
                marginRight: 0,
                borderRadius: 0,
                backgroundColor: "rgba(0,0,0,0)",
                boxShadow: "0 0 0 0 rgba(0,0,0,0)",
                backdropFilter: "blur(0px)",
              }
        }
        transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1] }}
      >
        <nav
          className="mx-auto flex h-[60px] max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8"
          aria-label="Main"
        >
          <a href="#top" className="flex items-center gap-2" aria-label="backstop home">
            <LogoMark />
            <span style={{ fontFamily: "var(--font-brand)", fontWeight: 700, fontSize: "17px", letterSpacing: "-0.035em" }}>Backstop</span>
          </a>
          <div className="hidden items-center gap-7 md:flex" suppressHydrationWarning>
            {["Docs", "Changelog", "Pricing", "Blog"].map((item) => (
              <a
                key={item}
                href={item === "Docs" ? "/docs" : `#${item.toLowerCase()}`}
                className="group relative text-sm text-text-secondary transition-colors hover:text-text-primary"
              >
                {item}
                <span className="absolute -bottom-2 left-1/2 h-px w-full origin-center -translate-x-1/2 scale-x-0 bg-danger transition-transform duration-200 group-hover:scale-x-100" />
              </a>
            ))}
          </div>
          <div className="flex min-w-0 items-center gap-3" suppressHydrationWarning>
            <a
              href={github.url}
              className="hidden items-center gap-1.5 rounded-md border border-border bg-bg-secondary px-2.5 py-1.5 font-mono text-xs text-text-secondary transition hover:border-[rgba(255,68,68,0.35)] hover:text-text-primary sm:flex"
              aria-label="View backstop on GitHub"
            >
              <IconBrandGithub size={16} stroke={1.5} />
              {github.stars === null ? "GitHub" : `${formatNumber(github.stars)} stars`}
            </a>
            <a href="#signin" className="hidden text-sm text-text-secondary transition hover:text-text-primary sm:block">
              Sign in
            </a>
            <GlowButton href="#cta" compact className="hidden sm:inline-flex">
              <span className="hidden sm:inline">Start free</span>
              <span className="sm:hidden">Start</span>
              <IconArrowRight size={16} stroke={1.5} />
            </GlowButton>
          </div>
        </nav>
      </motion.header>
    </div>
  );
}
