"use client";

import { useEffect, useRef, useState } from "react";
import anime from "animejs";
import { motion, useInView } from "framer-motion";
import { cn, formatNumber } from "@/lib/utils";

const ease = [0.16, 1, 0.3, 1] as const;

export function CountUpValue({ value, suffix = "", prefix = "" }: { value: number; suffix?: string; prefix?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, amount: 0.3 });
  const [display, setDisplay] = useState(0);

  useEffect(() => {
    if (!inView) return;
    if (value === 0) {
      setDisplay(0);
      return;
    }
    const controls = anime({
      targets: { n: 0 },
      n: value,
      duration: 1100,
      easing: "easeOutQuart",
      update(animation) {
        const current = animation.animatables[0].target as unknown as { n: number };
        setDisplay(Math.round(current.n));
      }
    });
    return () => controls.pause();
  }, [inView, value]);

  return (
    <span ref={ref}>
      {prefix}{formatNumber(display)}{suffix}
    </span>
  );
}

export function MetricsSection() {
  const stats = [
    { value: 2, prefix: "< ", suffix: "ms", label: "query intercept overhead" },
    { value: 60, prefix: "", suffix: "s", label: "average table restore" },
    { value: 4, prefix: "", suffix: "", label: "AST risk levels" },
    { value: 0, prefix: "", suffix: "", label: "regex SQL parsing paths" }
  ];

  return (
    <section className="border-y border-border bg-bg-secondary py-10">
      <div className="mx-auto grid max-w-7xl grid-cols-2 px-4 sm:px-6 md:grid-cols-4 lg:px-8">
        {stats.map((stat, index) => (
          <motion.div
            key={stat.label}
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true, amount: 0.3 }}
            transition={{ duration: 0.5, ease, delay: index * 0.09 }}
            className={cn("group px-5 py-6", index > 0 && "md:border-l md:border-border")}
          >
            <div className="font-serif text-5xl leading-none transition group-hover:text-danger group-hover:[text-shadow:0_0_20px_rgba(255,68,68,0.4)]">
              <CountUpValue value={stat.value} prefix={stat.prefix} suffix={stat.suffix} />
            </div>
            <div className="mt-3 text-sm text-text-secondary">{stat.label}</div>
          </motion.div>
        ))}
      </div>
    </section>
  );
}
