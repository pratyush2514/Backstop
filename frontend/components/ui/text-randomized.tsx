"use client";

import { useEffect, useRef, useState } from "react";

const CHARS = "abcdefghijklmnopqrstuvwxyz0123456789_-+=;:.,'\"(){}[]<>/*#@$%^&~|\\";
const rc = () => CHARS[Math.floor(Math.random() * CHARS.length)];

type Meta = { li: number; ci: number; ch: string };

/**
 * React-state-based scramble-then-reveal hook for multi-line code.
 * Returns the animated lines array and a `done` flag.
 * Pass `enabled=false` (e.g. reduced motion) to skip entirely.
 */
export function useScrambleLines(source: string, enabled: boolean) {
  const sourceLines = source.split("\n");

  const [lines, setLines] = useState<string[]>(() =>
    sourceLines.map(ln => ln.split("").map(ch => (/\s/.test(ch) ? ch : rc())).join(""))
  );
  const [done, setDone] = useState(false);
  const onceRef = useRef(false);

  useEffect(() => {
    if (!enabled) { setLines(sourceLines); setDone(true); return; }
    // Reset on each mount (component is keyed so this runs once per animation)
    onceRef.current = false;
    setDone(false);

    const flat: Meta[] = [];
    sourceLines.forEach((ln, li) => {
      ln.split("").forEach((ch, ci) => {
        if (!/\s/.test(ch)) flat.push({ li, ci, ch });
      });
    });

    // Working copy — pre-scrambled
    const W = sourceLines.map(ln =>
      ln.split("").map(ch => (/\s/.test(ch) ? ch : rc()))
    );

    const step = Math.max(1, Math.ceil(flat.length / 24));
    let rafId: number;
    let cancelled = false;
    let revealIdx = 0;
    let tick = 0;
    const SCRAMBLE_MS = 200;
    const t0 = performance.now();

    function frame(now: number) {
      if (cancelled) return;
      tick++;

      if (now - t0 < SCRAMBLE_MS) {
        flat.forEach(({ li, ci }) => { W[li][ci] = rc(); });
      } else {
        revealIdx = Math.min(revealIdx + step, flat.length);
        for (let i = 0; i < flat.length; i++) {
          const m = flat[i];
          if (i < revealIdx) {
            W[m.li][m.ci] = m.ch;
          } else if (tick % 2 === 0) {
            W[m.li][m.ci] = rc();
          }
        }
      }

      setLines(W.map(chars => chars.join("")));

      if (now - t0 >= SCRAMBLE_MS && revealIdx >= flat.length) {
        setDone(true);
        return;
      }
      rafId = requestAnimationFrame(frame);
    }

    rafId = requestAnimationFrame(frame);
    return () => { cancelled = true; cancelAnimationFrame(rafId); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return { lines, done };
}
