"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { cn } from "@/lib/utils";
import type { Root, Node, Folder as FolderNode } from "fumadocs-core/page-tree";
import { motion, AnimatePresence } from "framer-motion";
import {
  IconRocket, IconRobot, IconArrowsExchange, IconIdBadge2, IconPackage,
  IconStack2, IconCreditCard, IconInfinity, IconTrendingUp, IconCoins,
  IconShieldCheck, IconCamera, IconBolt, IconKey, IconBrandPython,
  IconBrandNodejs, IconServer, IconTerminal, IconApiApp, IconFileText,
  IconSettings, IconPlugConnected, IconSettingsAutomation, IconLockCheck
} from "@tabler/icons-react";
import { siCursor, siLangchain, siDocker, siSqlalchemy } from "simple-icons";

interface SidebarProps {
  tree: Root;
}

function BrandIcon({ path }: { path: string }) {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor" aria-hidden="true">
      <path d={path} />
    </svg>
  );
}

function getSidebarIcon(name: string) {
  const n = name.toLowerCase();
  if (n.includes("cursor")) return <BrandIcon path={siCursor.path} />;
  if (n.includes("langchain")) return <BrandIcon path={siLangchain.path} />;
  if (n.includes("docker")) return <BrandIcon path={siDocker.path} />;
  if (n.includes("sqlalchemy")) return <BrandIcon path={siSqlalchemy.path} />;
  
  if (n.includes("intro")) return <IconRocket size={18} stroke={1.5} />;
  if (n.includes("agent") || n.includes("mcp")) return <IconRobot size={18} stroke={1.5} />;
  if (n.includes("safety") || n.includes("risk")) return <IconShieldCheck size={18} stroke={1.5} />;
  if (n.includes("workflow") || n.includes("approval")) return <IconBolt size={18} stroke={1.5} />;
  if (n.includes("snapshot")) return <IconCamera size={18} stroke={1.5} />;
  if (n.includes("policy") || n.includes("config")) return <IconSettings size={18} stroke={1.5} />;
  if (n.includes("python")) return <IconBrandPython size={18} stroke={1.5} />;
  if (n.includes("node")) return <IconBrandNodejs size={18} stroke={1.5} />;
  if (n.includes("server")) return <IconServer size={18} stroke={1.5} />;
  if (n.includes("cli") || n.includes("terminal")) return <IconTerminal size={18} stroke={1.5} />;
  if (n.includes("api")) return <IconApiApp size={18} stroke={1.5} />;
  if (n.includes("integration")) return <IconPlugConnected size={18} stroke={1.5} />;
  if (n.includes("operation")) return <IconSettingsAutomation size={18} stroke={1.5} />;
  if (n.includes("security") || n.includes("credential")) return <IconKey size={18} stroke={1.5} />;
  if (n.includes("migrate")) return <IconArrowsExchange size={18} stroke={1.5} />;
  if (n.includes("verification")) return <IconIdBadge2 size={18} stroke={1.5} />;
  if (n.includes("product collection")) return <IconStack2 size={18} stroke={1.5} />;
  if (n.includes("product")) return <IconPackage size={18} stroke={1.5} />;
  if (n.includes("one-time")) return <IconCreditCard size={18} stroke={1.5} />;
  if (n.includes("subscription")) return <IconInfinity size={18} stroke={1.5} />;
  if (n.includes("usage")) return <IconTrendingUp size={18} stroke={1.5} />;
  if (n.includes("credit")) return <IconCoins size={18} stroke={1.5} />;
  return <IconFileText size={18} stroke={1.5} />;
}

function SidebarPage({ item, depth = 0 }: { item: Node; depth?: number }) {
  const pathname = usePathname();

  if (item.type === "separator") {
    return (
      <div className="mt-8 mb-2 px-4 font-mono text-[10px] font-bold tracking-[0.15em] text-white/30 uppercase">
        {item.name}
      </div>
    );
  }

  if (item.type === "folder") {
    return <SidebarFolder folder={item} depth={depth} />;
  }

  const active = pathname === item.url;
  const label = typeof item.name === "string" ? item.name : "";

  return (
    <Link
      href={item.url}
      className={cn(
        "relative flex w-full items-center gap-3 py-2 text-[14px] transition-all group",
        active ? "text-white font-medium" : "text-white/50 hover:text-white"
      )}
      style={{ paddingLeft: `${depth === 1 ? 16 : 16 + (depth - 1) * 12}px`, paddingRight: '16px' }}
    >
      {active && (
        <motion.div
          layoutId="sidebar-active"
          className="absolute left-[-1px] top-0 bottom-0 w-[2px] bg-red-500 rounded-r-full z-10"
          initial={false}
          transition={{ type: "spring", bounce: 0, duration: 0.3 }}
        />
      )}
      <div className={cn("flex shrink-0 items-center justify-center transition-colors", active ? "text-white" : "text-white/40 group-hover:text-white/80")}>
        {getSidebarIcon(label)}
      </div>
      <span className="truncate">{item.name}</span>
      {label.toLowerCase().includes("credit") && (
        <span className="ml-auto flex shrink-0 items-center justify-center rounded bg-[#1a2e1a] px-1.5 py-0.5 text-[10px] font-bold text-[#86e026]">
          NEW
        </span>
      )}
    </Link>
  );
}

function SidebarFolder({ folder, depth = 0 }: { folder: FolderNode; depth?: number }) {
  const pathname = usePathname();
  const hasActiveChild = folder.children.some(
    child => child.type === "page" && pathname.startsWith(child.url)
  ) || !!(folder.index && pathname.startsWith(folder.index.url));

  const [open, setOpen] = useState(hasActiveChild || depth === 0);

  if (depth === 0) {
    return (
      <div className="mb-8">
        <div className="px-4 pb-3 font-sans text-[15px] font-bold text-white/90">
          {folder.name}
        </div>
        <div className="relative ml-4 flex flex-col border-l border-white/10">
          {folder.index && (
            <SidebarPage item={folder.index} depth={depth + 1} />
          )}
          {folder.children.map((child) => (
            <SidebarPage key={String(child.name)} item={child} depth={depth + 1} />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      <button
        onClick={() => setOpen(!open)}
        className={cn(
          "flex w-full items-center gap-2 py-2 text-[14px] transition-colors text-left",
          hasActiveChild ? "text-white font-medium" : "text-white/50 hover:text-white"
        )}
        style={{ paddingLeft: `${16 + depth * 12}px`, paddingRight: '16px' }}
      >
        <svg
          width="12" height="12" viewBox="0 0 12 12" fill="none"
          className={cn("transition-transform duration-200 text-white/30", open && "rotate-90")}
          aria-hidden="true"
        >
          <path d="M4 2L8 6L4 10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {folder.name}
      </button>
      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: "easeInOut" }}
            className="overflow-hidden flex flex-col"
          >
            {folder.index && <SidebarPage item={folder.index} depth={depth + 1} />}
            {folder.children.map((child) => (
              <SidebarPage key={String(child.name)} item={child} depth={depth + 1} />
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export function Sidebar({ tree }: SidebarProps) {
  return (
    <aside className="fixed top-0 left-0 bottom-0 w-[260px] pt-[60px] overflow-y-auto border-r border-white/5 lg:bg-transparent bg-[#09090b]/80 backdrop-blur-xl hidden lg:block custom-scrollbar z-10">
      <div className="py-6 px-2 space-y-2">
        {tree.children.map((item) => (
          <SidebarPage key={String(item.name)} item={item} depth={0} />
        ))}
      </div>
    </aside>
  );
}
