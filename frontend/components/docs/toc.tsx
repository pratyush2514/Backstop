"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { TOCItemType } from "fumadocs-core/toc";

interface DocsTOCProps {
  items: TOCItemType[];
}

function flattenTOC(items: TOCItemType[]): TOCItemType[] {
  return items;
}

export function DocsTOC({ items }: DocsTOCProps) {
  const [activeId, setActiveId] = useState<string>("");
  const observerRef = useRef<IntersectionObserver | null>(null);
  const flat = flattenTOC(items);

  useEffect(() => {
    if (observerRef.current) observerRef.current.disconnect();

    const headings = flat
      .map(item => document.getElementById(item.url.replace("#", "")))
      .filter(Boolean) as HTMLElement[];

    observerRef.current = new IntersectionObserver(
      (entries) => {
        // We only want to set the active ID if it's actually entering the view at the top.
        // A simple approach is keeping track of the first intersecting element.
        const intersecting = entries.find(e => e.isIntersecting);
        if (intersecting) {
          setActiveId(intersecting.target.id);
        }
      },
      { rootMargin: "0px 0px -80% 0px", threshold: 1.0 }
    );

    headings.forEach(h => observerRef.current!.observe(h));
    return () => observerRef.current?.disconnect();
  }, [flat.map(i => i.url).join(",")]);

  if (!items.length) return null;

  return (
    <nav className="space-y-4" aria-label="On this page">
      <p className="font-mono text-[11px] font-bold tracking-[0.1em] text-white/40 uppercase">
        On this page
      </p>
      <ul className="space-y-2.5 relative border-l border-white/10 ml-1">
        {flat.map((item) => {
          const isActive = activeId === item.url.replace("#", "");
          return (
            <li key={item.url} className="relative">
              {isActive && (
                <div className="absolute left-[-1px] top-0 bottom-0 w-[2px] bg-red-500 rounded-full" />
              )}
              <a
                href={item.url}
                className={cn(
                  "block text-[13px] transition-colors leading-tight py-0.5",
                  item.depth === 3 ? "pl-7" : "pl-4",
                  isActive ? "text-white font-medium" : "text-white/50 hover:text-white"
                )}
                onClick={(e) => {
                  e.preventDefault();
                  const id = item.url.replace("#", "");
                  document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
                  setActiveId(id);
                }}
              >
                {item.title}
              </a>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
