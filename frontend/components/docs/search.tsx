"use client";

import { useEffect, useRef, useState } from "react";
import { useDocsSearch } from "fumadocs-core/search/client";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { IconSearch, IconX, IconFileText, IconLoader2 } from "@tabler/icons-react";

interface SearchDialogProps {
  open: boolean;
  onClose: () => void;
}

export function SearchDialog({ open, onClose }: SearchDialogProps) {
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const { search, setSearch, query: searchQuery } = useDocsSearch({
    type: "fetch",
    api: "/api/search",
  });

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 50);
    } else {
      setQuery("");
      setSearch("");
    }
  }, [open]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        open ? onClose() : undefined;
      }
      if (e.key === "Escape" && open) onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onClose]);

  if (!open) return null;

  const results = searchQuery.data && searchQuery.data !== "empty" ? searchQuery.data : [];
  const isLoading = searchQuery.isLoading;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] sm:pt-[15vh]" aria-modal="true" role="dialog">
      <div className="absolute inset-0 bg-[#09090b]/80 backdrop-blur-sm" onClick={onClose} />
      
      <div 
        className="relative w-full max-w-2xl bg-[#0e0e11] border border-white/10 rounded-xl shadow-2xl overflow-hidden mx-4"
        onClick={e => e.stopPropagation()}
      >
        {/* Input */}
        <div className="flex items-center px-4 py-3 gap-3 border-b border-white/10">
          <IconSearch size={18} stroke={1.5} className="text-white/40 shrink-0" />
          <input
            ref={inputRef}
            value={query}
            onChange={e => { setQuery(e.target.value); setSearch(e.target.value); }}
            placeholder="Search documentation..."
            className="flex-1 bg-transparent border-none outline-none text-[15px] text-white placeholder:text-white/40"
            spellCheck={false}
            autoComplete="off"
          />
          {query && (
            <button onClick={() => { setQuery(""); setSearch(""); inputRef.current?.focus(); }} className="text-white/40 hover:text-white p-1 rounded-md transition-colors shrink-0">
              <IconX size={16} stroke={2} />
            </button>
          )}
          {!query && (
            <div className="flex shrink-0 px-2 py-1 bg-white/5 border border-white/10 rounded text-[11px] font-mono text-white/40 uppercase tracking-widest cursor-pointer hover:bg-white/10 transition-colors" onClick={onClose}>
              Esc
            </div>
          )}
        </div>

        {/* Results */}
        <div className="max-h-[60vh] overflow-y-auto p-2">
          {isLoading && (
            <div className="flex items-center justify-center py-14 text-white/40 gap-3">
              <IconLoader2 size={18} className="animate-spin" />
              <span className="text-[14px]">Searching...</span>
            </div>
          )}

          {!isLoading && query && results.length === 0 && (
            <div className="flex flex-col items-center justify-center py-14 text-center">
              <p className="text-[14px] text-white/40">
                No results for <strong className="text-white font-medium">"{query}"</strong>
              </p>
            </div>
          )}

          {!isLoading && !query && (
            <div className="flex items-center justify-center py-16 text-center">
              <p className="text-[14px] text-white/40">Type to search across all documentation.</p>
            </div>
          )}

          {!isLoading && results.length > 0 && (
            <ul className="space-y-1">
              {results.map((result, i) => (
                <li key={result.id || result.url || i}>
                  <Link
                    href={result.url}
                    className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg hover:bg-white/5 transition-colors group"
                    onClick={onClose}
                  >
                    <div className="flex items-center gap-2">
                      <IconFileText size={16} stroke={1.5} className="text-white/40 group-hover:text-white/70 transition-colors shrink-0" />
                      <div 
                        className="text-[14px] font-medium text-white/80 group-hover:text-white transition-colors truncate [&>mark]:bg-red-500/20 [&>mark]:text-red-300 [&>mark]:rounded-sm [&>mark]:px-0.5 [&>mark]:font-semibold"
                        dangerouslySetInnerHTML={{ __html: result.content }}
                      />
                    </div>
                    {result.breadcrumbs && result.breadcrumbs.length > 0 && (
                      <div
                        className="text-[13px] text-white/40 pl-6 line-clamp-1"
                      >{result.breadcrumbs.join(" / ")}</div>
                    )}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* Footer hint */}
        <div className="flex items-center gap-4 px-4 py-3 border-t border-white/10 bg-white/[0.02]">
          <div className="flex items-center gap-2 text-[12px] text-white/40">
            <div className="flex items-center gap-1">
              <kbd className="flex items-center justify-center px-1.5 py-0.5 rounded border border-white/10 bg-white/5 font-mono text-[10px]">↑</kbd>
              <kbd className="flex items-center justify-center px-1.5 py-0.5 rounded border border-white/10 bg-white/5 font-mono text-[10px]">↓</kbd>
            </div>
            <span>navigate</span>
          </div>
          <div className="flex items-center gap-2 text-[12px] text-white/40">
            <kbd className="flex items-center justify-center px-1.5 py-0.5 rounded border border-white/10 bg-white/5 font-mono text-[10px]">↵</kbd>
            <span>open</span>
          </div>
          <div className="flex items-center gap-2 text-[12px] text-white/40">
            <kbd className="flex items-center justify-center px-1.5 py-0.5 rounded border border-white/10 bg-white/5 font-mono text-[10px]">Esc</kbd>
            <span>close</span>
          </div>
        </div>
      </div>
    </div>
  );
}
