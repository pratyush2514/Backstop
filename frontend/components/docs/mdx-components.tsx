import { type ReactNode, type HTMLAttributes } from "react";
import type { MDXComponents } from "mdx/types";
import { cn } from "@/lib/utils";
import { IconAlertTriangle, IconInfoCircle, IconShieldCheck, IconAlertCircle } from "@tabler/icons-react";
import { Pre, Tabs, Tab } from "./mdx-interactive";

/* ─── Heading anchors ─────────────────────────────────────────── */

function anchor(Tag: "h1" | "h2" | "h3" | "h4") {
  return function Heading({ children, id, ...props }: HTMLAttributes<HTMLHeadingElement>) {
    return (
      <Tag id={id} {...props} className={cn("docs-heading group scroll-mt-24", `docs-${Tag}`)}>
        {children}
        {id && (
          <a 
            href={`#${id}`} 
            className="absolute -left-6 top-1/2 -translate-y-1/2 opacity-0 text-white/30 transition-opacity hover:text-white group-hover:opacity-100 no-underline" 
            aria-hidden="true"
          >
            #
          </a>
        )}
      </Tag>
    );
  };
}

/* ─── Callout ─────────────────────────────────────────────────── */

type CalloutType = "info" | "warning" | "danger" | "success" | "note";

const calloutConfig: Record<CalloutType, { icon: ReactNode; className: string; iconColor: string }> = {
  info:    { icon: <IconInfoCircle size={18} stroke={1.5} />,    className: "border-blue-500/20 bg-blue-500/5", iconColor: "text-blue-400" },
  warning: { icon: <IconAlertTriangle size={18} stroke={1.5} />, className: "border-yellow-500/20 bg-yellow-500/5", iconColor: "text-yellow-400" },
  danger:  { icon: <IconAlertCircle size={18} stroke={1.5} />,   className: "border-red-500/30 bg-red-500/5", iconColor: "text-red-500" },
  success: { icon: <IconShieldCheck size={18} stroke={1.5} />,   className: "border-green-500/20 bg-green-500/5", iconColor: "text-green-400" },
  note:    { icon: <IconInfoCircle size={18} stroke={1.5} />,    className: "border-white/10 bg-white/5", iconColor: "text-white/60" },
};

export function Callout({
  type = "info",
  title,
  children,
}: {
  type?: CalloutType;
  title?: string;
  children: ReactNode;
}) {
  const { icon, className, iconColor } = calloutConfig[type];
  return (
    <aside className={cn("my-8 flex gap-4 rounded-xl border p-5 shadow-lg backdrop-blur-md relative overflow-hidden", className)}>
      <div className={cn("mt-0.5 shrink-0", iconColor)}>{icon}</div>
      <div className="flex-1">
        {title && <p className="mb-2 font-semibold tracking-tight text-white">{title}</p>}
        <div className="text-[15px] leading-relaxed text-white/70 prose-a:text-white hover:prose-a:text-white/80">{children}</div>
      </div>
      <div className={cn("absolute inset-y-0 left-0 w-1", type === "danger" ? "bg-red-500" : "bg-transparent")} />
    </aside>
  );
}

/* ─── Steps ───────────────────────────────────────────────────── */

export function Steps({ children }: { children: ReactNode }) {
  return (
    <ol className="my-10 ml-4 border-l border-white/10 space-y-12 pl-8 relative">
      {children}
    </ol>
  );
}

export function Step({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <li className="relative group">
      <div className="absolute -left-[41px] top-1 flex h-6 w-6 items-center justify-center rounded-full border border-white/20 bg-[#09090b] shadow-sm transition-colors group-hover:border-white/40">
        <div className="h-1.5 w-1.5 rounded-full bg-white/50 group-hover:bg-white" />
      </div>
      {title && <div className="mb-2 text-[17px] font-semibold text-white tracking-tight">{title}</div>}
      <div className="text-[15px] leading-relaxed text-white/70">{children}</div>
    </li>
  );
}

/* ─── API Method ──────────────────────────────────────────────── */

const methodColor: Record<string, string> = {
  GET:    "bg-blue-500/10 text-blue-400 border-blue-500/20",
  POST:   "bg-green-500/10 text-green-400 border-green-500/20",
  PUT:    "bg-yellow-500/10 text-yellow-400 border-yellow-500/20",
  DELETE: "bg-red-500/10 text-red-400 border-red-500/20",
  PATCH:  "bg-purple-500/10 text-purple-400 border-purple-500/20",
};

export function ApiMethod({
  method,
  path,
  auth,
  children,
}: {
  method: string;
  path: string;
  auth?: string;
  children?: ReactNode;
}) {
  return (
    <div className="my-8 rounded-xl border border-white/10 bg-[#0c0c0e] shadow-xl overflow-hidden">
      <div className="flex items-center flex-wrap gap-4 p-4 border-b border-white/5 bg-white/[0.02]">
        <span className={cn("px-2.5 py-1 text-xs font-bold rounded border tracking-wider", methodColor[method] ?? methodColor.GET)}>
          {method}
        </span>
        <code className="font-mono text-[14px] text-white/90">{path}</code>
        {auth && <span className="ml-auto text-xs text-white/40 border border-white/10 px-2 py-1 rounded bg-white/5 flex items-center gap-1"><IconShieldCheck size={12}/> {auth}</span>}
      </div>
      {children && <div className="p-5 text-[15px] text-white/70">{children}</div>}
    </div>
  );
}

/* ─── Property table ──────────────────────────────────────────── */

interface Property {
  name: string;
  type: string;
  required?: boolean;
  default?: string;
  description: string;
}

export function PropertyTable({ properties }: { properties: Property[] }) {
  return (
    <div className="my-8 w-full overflow-x-auto rounded-xl border border-white/10 shadow-lg">
      <table className="w-full text-left text-[14px]">
        <thead>
          <tr className="border-b border-white/10 bg-white/[0.02]">
            <th className="px-4 py-3 font-semibold text-white/90">Parameter</th>
            <th className="px-4 py-3 font-semibold text-white/90">Type</th>
            <th className="px-4 py-3 font-semibold text-white/90">Description</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-white/5 bg-[#09090b]">
          {properties.map((p) => (
            <tr key={p.name} className="hover:bg-white/[0.01] transition-colors">
              <td className="px-4 py-4 align-top">
                <div className="flex items-center gap-2">
                  <code className="rounded border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-[13px] text-white/90">{p.name}</code>
                  {p.required ? (
                    <span className="text-[11px] text-red-400 font-medium tracking-wide">REQUIRED</span>
                  ) : (
                    <span className="text-[11px] text-white/30 font-medium tracking-wide">OPTIONAL</span>
                  )}
                </div>
                {p.default && <div className="mt-1 text-[12px] text-white/40 font-mono">default: {p.default}</div>}
              </td>
              <td className="px-4 py-4 align-top">
                <code className="text-blue-400 font-mono text-[13px]">{p.type}</code>
              </td>
              <td className="px-4 py-4 align-top text-[14.5px] leading-relaxed text-white/70">
                {p.description}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/* ─── Risk badge ──────────────────────────────────────────────── */

const riskConfig: Record<string, string> = {
  SAFE:            "bg-green-500/10 text-green-400 border-green-500/20",
  LOW:             "bg-blue-500/10 text-blue-400 border-blue-500/20",
  HIGH:            "bg-yellow-500/10 text-yellow-400 border-yellow-500/20 animate-pulse-slow",
  CRITICAL:        "bg-red-500/10 text-red-500 border-red-500/30 font-bold shadow-[0_0_15px_rgba(255,68,68,0.2)]",
  IMPACT_CRITICAL: "bg-red-500/20 text-red-400 border-red-500/40",
};

export function RiskBadge({ level }: { level: string }) {
  return (
    <span className={cn("px-2 py-0.5 rounded border text-[11px] tracking-wider", riskConfig[level] ?? riskConfig.SAFE)}>
      {level}
    </span>
  );
}

/* ─── Terminal block ──────────────────────────────────────────── */

export function Terminal({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <div className="my-8 rounded-xl border border-white/10 bg-[#0c0c0e] shadow-2xl overflow-hidden relative">
      <div className="absolute inset-0 bg-gradient-to-br from-blue-500/5 to-transparent pointer-events-none" />
      <div className="flex items-center gap-3 px-4 py-3 border-b border-white/5 bg-white/[0.02]">
        <div className="flex gap-1.5">
          <div className="w-3 h-3 rounded-full bg-white/20" />
          <div className="w-3 h-3 rounded-full bg-white/20" />
          <div className="w-3 h-3 rounded-full bg-white/20" />
        </div>
        {title && <span className="font-mono text-[13px] text-white/50">{title}</span>}
      </div>
      <pre className="p-5 overflow-x-auto text-[14px] leading-loose font-mono text-white/90">
        {children}
      </pre>
    </div>
  );
}

/* ─── Re-export interactive components ────────────────────────── */

export { Pre, Tabs, Tab };

/* ─── getMDXComponents ────────────────────────────────────────── */

export function getMDXComponents(components: MDXComponents): MDXComponents {
  return ({
    h1: anchor("h1"),
    h2: anchor("h2"),
    h3: anchor("h3"),
    h4: anchor("h4"),
    pre: Pre,

    code: ({ children, ...props }: HTMLAttributes<HTMLElement>) => (
      <code {...props} className={cn("rounded border border-white/10 bg-white/[0.03] px-[0.3em] py-[0.2em] font-mono text-[0.85em] text-red-400", props.className)}>
        {children}
      </code>
    ),

    table: (props: HTMLAttributes<HTMLTableElement>) => (
      <div className="my-8 w-full overflow-x-auto rounded-xl border border-white/10 shadow-lg">
        <table {...props} className="w-full text-left text-[14.5px]" />
      </div>
    ),
    th: (props: HTMLAttributes<HTMLTableCellElement>) => (
      <th {...props} className="border-b border-white/10 bg-white/[0.02] px-4 py-3 font-semibold text-white/90" />
    ),
    td: (props: HTMLAttributes<HTMLTableCellElement>) => (
      <td {...props} className="border-b border-white/5 px-4 py-3 text-white/70" />
    ),

    blockquote: (props: HTMLAttributes<HTMLQuoteElement>) => (
      <blockquote {...props} className="my-8 border-l-2 border-white/20 pl-5 italic text-white/60 text-[15.5px]" />
    ),

    p: (props: HTMLAttributes<HTMLParagraphElement>) => (
      <p {...props} className="leading-loose text-[16px] text-white/70 mb-6" />
    ),

    ul: (props: HTMLAttributes<HTMLUListElement>) => (
      <ul {...props} className="list-disc list-outside ml-5 space-y-2 text-[15.5px] text-white/70 mb-6 marker:text-white/30" />
    ),

    ol: (props: HTMLAttributes<HTMLOListElement>) => (
      <ol {...props} className="list-decimal list-outside ml-5 space-y-2 text-[15.5px] text-white/70 mb-6 marker:text-white/30" />
    ),

    a: (props: HTMLAttributes<HTMLAnchorElement>) => (
      <a {...props} className="text-[#ff4444] decoration-[#ff4444]/40 underline-offset-4 hover:decoration-[#ff4444] transition-colors" />
    ),

    Callout,
    Steps,
    Step,
    Tabs,
    Tab,
    ApiMethod,
    PropertyTable,
    RiskBadge,
    Terminal,

    ...components,
  }) as MDXComponents;
}
