"use client";

import { useEffect, useRef } from "react";
import gsap from "gsap";

type Props = {
  dotSize?: number;
  gap?: number;
  baseColor?: string;
  proximity?: number;
  shockRadius?: number;
  shockStrength?: number;
  resistance?: number;
  returnDuration?: number;
  className?: string;
};

type Dot = {
  x: number;
  y: number;
  ox: number;
  oy: number;
  vx: number;
  vy: number;
  tween: gsap.core.Tween | null;
};

export default function DotGridBg({
  dotSize = 5,
  gap = 24,
  baseColor = "#8B949E",
  proximity = 120,
  shockRadius = 250,
  shockStrength = 5,
  resistance = 750,
  returnDuration = 1.5,
  className,
}: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    let dots: Dot[] = [];
    const mouse = { x: -9999, y: -9999, vx: 0, vy: 0, lx: -9999, ly: -9999 };
    let dpr = window.devicePixelRatio || 1;
    let rafId: number;

    function buildGrid(w: number, h: number) {
      dots.forEach(d => d.tween?.kill());
      dots = [];
      const step = dotSize + gap;
      const cols = Math.ceil(w / step) + 1;
      const rows = Math.ceil(h / step) + 1;
      for (let r = 0; r < rows; r++) {
        for (let c = 0; c < cols; c++) {
          const ox = c * step;
          const oy = r * step;
          dots.push({ x: ox, y: oy, ox, oy, vx: 0, vy: 0, tween: null });
        }
      }
    }

    function resize() {
      dpr = window.devicePixelRatio || 1;
      const w = canvas!.offsetWidth;
      const h = canvas!.offsetHeight;
      canvas!.width = w * dpr;
      canvas!.height = h * dpr;
      ctx!.scale(dpr, dpr);
      buildGrid(w, h);
    }

    function parseColor(hex: string): { r: number; g: number; b: number } {
      const n = parseInt(hex.replace("#", ""), 16);
      return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
    }

    const { r, g, b } = parseColor(baseColor);
    const speed = Math.hypot;

    function draw() {
      rafId = requestAnimationFrame(draw);
      const w = canvas!.offsetWidth;
      const h = canvas!.offsetHeight;
      ctx!.clearRect(0, 0, w, h);

      const mv = speed(mouse.vx, mouse.vy);

      for (const dot of dots) {
        const dx = dot.x - mouse.x;
        const dy = dot.y - mouse.y;
        const dist = Math.sqrt(dx * dx + dy * dy) || 0.001;

        if (dist < proximity) {
          dot.tween?.kill();
          dot.tween = null;
          const force = ((proximity - dist) / proximity) * 10;
          dot.vx += (dx / dist) * force;
          dot.vy += (dy / dist) * force;
        }

        if (mv > 4 && dist < shockRadius) {
          dot.tween?.kill();
          dot.tween = null;
          const sf = (1 - dist / shockRadius) * shockStrength * (mv / 8);
          dot.vx += (dx / dist) * sf;
          dot.vy += (dy / dist) * sf;
        }

        if (dot.tween === null) {
          const sx = (dot.ox - dot.x) / (resistance * 0.1);
          const sy = (dot.oy - dot.y) / (resistance * 0.1);
          dot.vx = (dot.vx + sx) * 0.88;
          dot.vy = (dot.vy + sy) * 0.88;
          dot.x += dot.vx;
          dot.y += dot.vy;

          const disp = Math.sqrt((dot.x - dot.ox) ** 2 + (dot.y - dot.oy) ** 2);
          if (disp < 0.5 && mv < 1) {
            dot.x = dot.ox;
            dot.y = dot.oy;
            dot.vx = 0;
            dot.vy = 0;
          }
        }

        const disp = Math.sqrt((dot.x - dot.ox) ** 2 + (dot.y - dot.oy) ** 2);
        const alpha = Math.min(0.12 + disp * 0.025, 0.65);

        ctx!.beginPath();
        ctx!.arc(dot.x, dot.y, dotSize / 2, 0, Math.PI * 2);
        ctx!.fillStyle = `rgba(${r},${g},${b},${alpha})`;
        ctx!.fill();
      }

      mouse.vx = mouse.x - mouse.lx;
      mouse.vy = mouse.y - mouse.ly;
      mouse.lx = mouse.x;
      mouse.ly = mouse.y;
    }

    function onMouseMove(e: MouseEvent) {
      const rect = canvas!.getBoundingClientRect();
      mouse.x = e.clientX - rect.left;
      mouse.y = e.clientY - rect.top;
    }

    function onMouseLeave() {
      mouse.x = -9999;
      mouse.y = -9999;
      for (const dot of dots) {
        dot.tween?.kill();
        dot.tween = gsap.to(dot, {
          x: dot.ox,
          y: dot.oy,
          vx: 0,
          vy: 0,
          duration: returnDuration,
          ease: "elastic.out(1, 0.5)",
          onComplete() { dot.tween = null; },
        });
      }
    }

    const section = canvas.closest("section") ?? canvas.parentElement;
    section?.addEventListener("mousemove", onMouseMove, { passive: true });
    section?.addEventListener("mouseleave", onMouseLeave, { passive: true });

    const ro = new ResizeObserver(resize);
    ro.observe(canvas);
    resize();
    draw();

    return () => {
      cancelAnimationFrame(rafId);
      ro.disconnect();
      dots.forEach(d => d.tween?.kill());
      section?.removeEventListener("mousemove", onMouseMove);
      section?.removeEventListener("mouseleave", onMouseLeave);
    };
  }, [dotSize, gap, baseColor, proximity, shockRadius, shockStrength, resistance, returnDuration]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className={className}
      style={{ width: "100%", height: "100%", pointerEvents: "none" }}
    />
  );
}
