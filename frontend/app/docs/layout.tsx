"use client";

import { type ReactNode, useState, useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { IconBrandGithub, IconSearch, IconMenu2 } from "@tabler/icons-react";
import { Sidebar } from "@/components/docs/sidebar";
import { SearchDialog } from "@/components/docs/search";
import { source } from "@/source";
import { cn } from "@/lib/utils";

const NAV_SECTIONS = [
  { label: "Getting Started", href: "/docs/getting-started/introduction" },
  { label: "Concepts",        href: "/docs/concepts/safety-model" },
  { label: "SDKs",            href: "/docs/sdks/python" },
  { label: "MCP Server",      href: "/docs/mcp/overview" },
  { label: "CLI",             href: "/docs/cli/overview" },
  { label: "API Reference",   href: "/docs/api-reference/authentication" },
];

function DocsHeader({ onSearchOpen, onMenuToggle }: { onSearchOpen: () => void, onMenuToggle: () => void }) {
  const pathname = usePathname();
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 10);
    window.addEventListener("scroll", onScroll);
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header className={cn(
      "fixed top-0 inset-x-0 z-40 h-[60px] transition-all duration-300",
      scrolled ? "bg-[#09090b]/80 backdrop-blur-xl border-b border-white/5 shadow-md" : "bg-transparent border-b border-transparent"
    )}>
      <div className="flex items-center justify-between h-full px-4 lg:px-8 max-w-[1600px] mx-auto gap-4">
        {/* Left: mobile menu + logo */}
        <div className="flex items-center gap-3 min-w-0">
          <button onClick={onMenuToggle} className="lg:hidden p-1.5 text-white/70 hover:text-white hover:bg-white/5 rounded-md">
            <IconMenu2 size={20} />
          </button>
          <Link href="/" className="flex items-center gap-2 shrink-0 group">
            <LogoMark />
            <span className="font-sans font-bold text-[16px] tracking-tight text-white group-hover:text-white/90 transition-colors">Backstop</span>
          </Link>
          <span className="text-white/20 text-lg font-light hidden sm:inline-block">/</span>
          <span className="text-white/50 text-[14px] font-medium hidden sm:inline-block">Docs</span>
        </div>

        {/* Center: section tabs */}
        <nav className="hidden lg:flex items-center justify-center gap-1 flex-1">
          {NAV_SECTIONS.map((s) => {
            const sectionKey = s.href.split("/")[2];
            const active = pathname.includes(`/docs/${sectionKey}`);
            return (
              <Link
                key={s.href}
                href={s.href}
                className={cn(
                  "px-3 py-1.5 text-[13px] font-medium rounded-full transition-all duration-200",
                  active ? "text-white bg-white/10 shadow-sm" : "text-white/50 hover:text-white hover:bg-white/5"
                )}
              >
                {s.label}
              </Link>
            );
          })}
        </nav>

        {/* Right: search + github */}
        <div className="flex items-center gap-2 lg:gap-3 shrink-0">
          <button
            onClick={onSearchOpen}
            className="flex items-center gap-2 px-3 py-1.5 rounded-full border border-white/10 bg-white/5 hover:bg-white/10 hover:border-white/20 text-white/50 hover:text-white transition-all text-[13px]"
            aria-label="Search documentation"
          >
            <IconSearch size={14} stroke={2} />
            <span className="hidden sm:inline">Search...</span>
            <kbd className="hidden sm:inline-flex items-center justify-center px-1.5 py-0.5 border border-white/10 rounded bg-[#09090b] text-[10px] font-mono ml-2">⌘K</kbd>
          </button>
          <a
            href="https://github.com/pratyush/backstop"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center justify-center w-8 h-8 rounded-full text-white/50 hover:text-white hover:bg-white/10 transition-colors"
            aria-label="View on GitHub"
          >
            <IconBrandGithub size={18} stroke={1.5} />
          </a>
        </div>
      </div>
    </header>
  );
}

function LogoMark() {
  return (
    <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true" className="group-hover:scale-105 transition-transform">
      <path d="M14 3L23 6.7V13.8C23 19.3 19.2 23.5 14 25C8.8 23.5 5 19.3 5 13.8V6.7L14 3Z" stroke="#FF4444" strokeWidth="1.6" />
      <path d="M10 10.2C10 8.9 11.8 7.9 14 7.9C16.2 7.9 18 8.9 18 10.2C18 11.5 16.2 12.5 14 12.5C11.8 12.5 10 11.5 10 10.2Z" stroke="#E6EDF3" strokeWidth="1.3" />
      <path d="M10 10.4V16.4C10 17.7 11.8 18.7 14 18.7C16.2 18.7 18 17.7 18 16.4V10.4" stroke="#E6EDF3" strokeWidth="1.3" />
      <path d="M7.5 20.3L20.8 6.9" stroke="#FF4444" strokeWidth="1.2" />
    </svg>
  );
}

export default function DocsLayout({ children }: { children: ReactNode }) {
  const [searchOpen, setSearchOpen] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const tree = source.pageTree;

  return (
    <div className="flex flex-col min-h-screen bg-[#09090b] text-white selection:bg-red-500/30 selection:text-white relative" suppressHydrationWarning>
      {/* Background ambient glow */}
      <div className="fixed top-[-20%] left-[-10%] w-[50%] h-[50%] rounded-full bg-red-500/5 blur-[120px] pointer-events-none" />
      <div className="fixed top-[20%] right-[-10%] w-[40%] h-[40%] rounded-full bg-blue-500/5 blur-[120px] pointer-events-none" />

      <DocsHeader onSearchOpen={() => setSearchOpen(true)} onMenuToggle={() => setMobileMenuOpen(!mobileMenuOpen)} />
      
      <div className="flex flex-1 pt-[60px] max-w-[1600px] w-full mx-auto">
        {/* Sidebar wrapper handles mobile too (currently just hidden on mobile in sidebar.tsx, but we can expand that later) */}
        <div className={cn("lg:block", mobileMenuOpen ? "block fixed inset-0 z-30 pt-[60px] bg-[#09090b]" : "hidden")}>
          <Sidebar tree={tree} />
        </div>
        
        <main className="flex-1 min-w-0 lg:ml-[260px] relative z-0">
          {children}
        </main>
      </div>
      <SearchDialog open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  );
}
