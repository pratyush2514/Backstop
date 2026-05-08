"use client";

import { cn } from "@/lib/utils";
import { motion } from "framer-motion";
import { forwardRef, type RefObject, useEffect, useId, useState } from "react";

interface AnimatedBeamProps {
  containerRef: RefObject<HTMLDivElement>;
  fromRef: RefObject<HTMLDivElement>;
  toRef: RefObject<HTMLDivElement>;
  className?: string;
  curvature?: number;
  reverse?: boolean;
  duration?: number;
  delay?: number;
  pathColor?: string;
  pathWidth?: number;
  pathOpacity?: number;
  gradientStartColor?: string;
  gradientStopColor?: string;
  startXOffset?: number;
  startYOffset?: number;
  endXOffset?: number;
  endYOffset?: number;
  dotted?: boolean;
  dotSpacing?: number;
}

export function AnimatedBeam({
  containerRef,
  fromRef,
  toRef,
  className,
  curvature = 0,
  reverse = false,
  duration = Math.random() * 3 + 4,
  delay = 0,
  pathColor = "#21262d",
  pathWidth = 2,
  pathOpacity = 0.4,
  gradientStartColor = "#FF4444",
  gradientStopColor = "#ff8080",
  startXOffset = 0,
  startYOffset = 0,
  endXOffset = 0,
  endYOffset = 0,
  dotted = false,
  dotSpacing = 4,
}: AnimatedBeamProps) {
  const id = useId();
  const [pathD, setPathD] = useState("");
  const [svgDims, setSvgDims] = useState({ width: 0, height: 0 });

  const gradientCoords = reverse
    ? { x1: ["90%", "-10%"], x2: ["100%", "0%"], y1: ["0%", "0%"], y2: ["0%", "0%"] }
    : { x1: ["10%", "110%"], x2: ["0%", "100%"], y1: ["0%", "0%"], y2: ["0%", "0%"] };

  useEffect(() => {
    function update() {
      if (!containerRef.current || !fromRef.current || !toRef.current) return;
      const cRect = containerRef.current.getBoundingClientRect();
      const aRect = fromRef.current.getBoundingClientRect();
      const bRect = toRef.current.getBoundingClientRect();

      setSvgDims({ width: cRect.width, height: cRect.height });

      const sx = aRect.left - cRect.left + aRect.width / 2 + startXOffset;
      const sy = aRect.top - cRect.top + aRect.height / 2 + startYOffset;
      const ex = bRect.left - cRect.left + bRect.width / 2 + endXOffset;
      const ey = bRect.top - cRect.top + bRect.height / 2 + endYOffset;
      const cy = sy - curvature;

      setPathD(`M ${sx},${sy} Q ${(sx + ex) / 2},${cy} ${ex},${ey}`);
    }

    const ro = new ResizeObserver(update);
    if (containerRef.current) ro.observe(containerRef.current);
    update();
    return () => ro.disconnect();
  }, [containerRef, fromRef, toRef, curvature, startXOffset, startYOffset, endXOffset, endYOffset]);

  return (
    <svg
      fill="none"
      width={svgDims.width}
      height={svgDims.height}
      viewBox={`0 0 ${svgDims.width} ${svgDims.height}`}
      className={cn("pointer-events-none absolute left-0 top-0", className)}
    >
      <path
        d={pathD}
        stroke={pathColor}
        strokeWidth={pathWidth}
        strokeOpacity={pathOpacity}
        strokeLinecap="round"
        strokeDasharray={dotted ? `${dotSpacing} ${dotSpacing}` : undefined}
        fill="none"
      />
      <path
        d={pathD}
        stroke={`url(#${id})`}
        strokeWidth={pathWidth}
        strokeLinecap="round"
        strokeDasharray={dotted ? `${dotSpacing} ${dotSpacing}` : undefined}
        fill="none"
      />
      <defs>
        <motion.linearGradient
          id={id}
          gradientUnits="userSpaceOnUse"
          initial={{ x1: "0%", x2: "0%", y1: "0%", y2: "0%" }}
          animate={gradientCoords}
          transition={{
            delay,
            duration,
            ease: [0.16, 1, 0.3, 1],
            repeat: Infinity,
            repeatDelay: 0,
          }}
        >
          <stop stopColor={gradientStartColor} stopOpacity="0" />
          <stop stopColor={gradientStartColor} />
          <stop offset="32.5%" stopColor={gradientStopColor} />
          <stop offset="100%" stopColor={gradientStopColor} stopOpacity="0" />
        </motion.linearGradient>
      </defs>
    </svg>
  );
}

export const Circle = forwardRef<HTMLDivElement, { className?: string; children?: React.ReactNode }>(
  ({ className, children }, ref) => (
    <div
      ref={ref}
      className={cn(
        "relative z-10 flex size-12 items-center justify-center rounded-full border border-border bg-bg-elevated p-2.5",
        className
      )}
    >
      {children}
    </div>
  )
);
Circle.displayName = "Circle";
