"use client";

import { motion } from "framer-motion";
import { IconRefresh, IconShieldCheck } from "@tabler/icons-react";
import { IsoTerminal } from "@/components/iso-icons";
import { RevealSection, SectionHeading, ease } from "./ui";

export function HowItWorks() {
  const steps = [
    ["01", "Start", "backstop-oss up", "Brings up the gateway, sync sidecar, PostgreSQL, and MinIO for the local OSS flow."],
    ["02", "Intercept", "# backstop classifies this before gateway execution\nDROP TABLE users;", "AST parsing classifies the query as CRITICAL and the gateway checks for a latest recoverable snapshot. No regex. No guessing."],
    [
      "03",
      "Recover",
      "backstop restore \\\n  --db $DATABASE_URL \\\n  --storage s3://prod-snapshots \\\n  --snapshot-id snap_a3f9 \\\n  --table users",
      "The restore engine rebuilds a recovered table from the snapshot manifest and Parquet rows, with preview and validation steps available first."
    ]
  ];

  return (
    <RevealSection className="py-24">
      <SectionHeading before="Install once. Protected" red="forever." />
      <div className="relative mx-auto mt-14 grid max-w-7xl gap-5 px-4 sm:px-6 lg:grid-cols-3 lg:px-8">
        <svg className="pointer-events-none absolute left-[18%] right-[18%] top-[104px] hidden h-2 lg:block" viewBox="0 0 100 2" preserveAspectRatio="none">
          <motion.path
            d="M0 1H38M62 1H100"
            stroke="#FF4444"
            strokeWidth="0.35"
            strokeDasharray="100"
            initial={{ strokeDashoffset: 100 }}
            whileInView={{ strokeDashoffset: 0 }}
            viewport={{ once: true, amount: 0.5 }}
            transition={{ duration: 0.8, ease: "easeInOut" }}
          />
        </svg>
        {steps.map(([num, title, code, body], index) => (
          <motion.div
            key={title}
            initial={{ opacity: 0, y: 18 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true, amount: 0.3 }}
            transition={{ delay: index * 0.18, duration: 0.4, ease }}
            className="relative rounded-xl border border-border bg-bg-secondary p-6"
          >
            <div className="pointer-events-none absolute right-5 top-5 font-mono text-[72px] leading-none text-border/70">{num}</div>
            <div className="relative z-10 flex h-10 w-10 items-center justify-center rounded-md border border-border bg-bg-primary text-danger">
              {index === 0 ? <IsoTerminal className="h-5 w-5" /> : index === 1 ? <IconShieldCheck size={20} stroke={1.5} /> : <IconRefresh size={20} stroke={1.5} />}
            </div>
            <h3 className="relative z-10 mt-8 text-xl font-semibold">{title}</h3>
            <pre className="relative z-10 mt-5 overflow-x-auto whitespace-pre-wrap rounded-lg border border-border bg-bg-primary p-4 font-mono text-sm leading-6 text-danger">{code}</pre>
            <p className="mt-4 leading-7 text-text-secondary">{body}</p>
          </motion.div>
        ))}
      </div>
    </RevealSection>
  );
}
