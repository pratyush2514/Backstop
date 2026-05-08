"use client";

import { motion } from "framer-motion";
import { IconArrowRight } from "@tabler/icons-react";
import { BlurWords, GlowButton } from "./ui";

export function CTASection() {
  return (
    <section id="cta" className="relative flex min-h-screen items-center justify-center overflow-hidden px-4 py-24 text-center">
      <div className="cta-aurora absolute inset-0" />
      <div className="relative max-w-4xl">
        <h2 className="font-serif text-[clamp(56px,8vw,96px)] leading-[0.95]" style={{ textWrap: "balance" } as React.CSSProperties}>
            <BlurWords text="Your next deployment shouldn't be your" delay={0.05} />
            {" "}
            <motion.span
              className="red-italic"
              initial={{ opacity: 0, filter: "blur(12px)", y: 6 }}
              whileInView={{ opacity: 1, filter: "blur(0px)", y: 0 }}
              viewport={{ once: true, amount: 0 }}
              transition={{ duration: 0.55, ease: [0.16, 1, 0.3, 1], delay: 0.58 }}
              style={{ display: "inline-block" }}
            >
              last.
            </motion.span>
        </h2>
        <p className="mx-auto mt-6 max-w-2xl text-lg leading-8 text-text-secondary">
          Add a safer control layer in front of production SQL. Approve risky writes, verify recovery readiness, and
          restore supported table snapshots without leaving your own infrastructure.
        </p>
        <div className="mt-9 flex justify-center">
          <GlowButton href="#pricing" large>Start protecting your database <IconArrowRight size={20} stroke={1.5} /></GlowButton>
        </div>
        <p className="mt-5 text-xs text-text-tertiary">Free forever for self-hosted. No credit card required.</p>
      </div>
    </section>
  );
}
