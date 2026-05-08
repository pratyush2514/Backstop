import { codeToHtml } from "shiki";

export type CodeTab = {
  id: string;
  label: string;
  language: string;
  highlightLine: number;
  source: string;
  html: string;
};

const snippets = [
  {
    id: "python",
    label: "Python",
    language: "python",
    highlightLine: 6,
    source: `import os
import psycopg2
import backstop

raw_conn = psycopg2.connect(os.environ["DATABASE_URL"])
db = backstop.guard(
    conn=raw_conn,
    storage="s3://prod-snapshots@http://localhost:9000",
    actor="gpt-4-agent",
    mode="protect",
)

db.execute("DROP TABLE users")
db.commit()`
  },
  {
    id: "node",
    label: "Node.js",
    language: "ts",
    highlightLine: 8,
    source: `import { BackstopClient } from "@backstop/client";

const backstop = new BackstopClient({
  url: process.env.BACKSTOP_URL,
  token: process.env.BACKSTOP_TOKEN,
  agentId: process.env.BACKSTOP_AGENT_ID ?? "cursor-local",
});

await backstop.query("DELETE FROM payments");`
  },
  {
    id: "go",
    label: "Go",
    language: "go",
    highlightLine: 6,
    source: `// Route agent SQL through the backstop gateway.
req := gateway.QueryRequest{
    AgentID: "codex-dev-agent",
    SQL:     "TRUNCATE orders",
}

decision := policy.Decide(analyzeSQL(req.SQL))`
  },
  {
    id: "django",
    label: "Django",
    language: "python",
    highlightLine: 9,
    source: `from django.db import connection
import backstop

def guarded_cursor(actor: str):
    raw = connection.connection
    if raw is None:
        connection.ensure_connection()
        raw = connection.connection
    return backstop.guard(raw, storage="s3://prod-snapshots", actor=actor).cursor()`
  },
  {
    id: "prisma",
    label: "Prisma",
    language: "ts",
    highlightLine: 3,
    source: `// Prisma protection routes raw SQL through @backstop/client today.
// Native Prisma middleware can call this before destructive $executeRaw.
await backstop.query("UPDATE users SET role = 'admin'");`
  }
] as const;

let _cache: CodeTab[] | null = null;

export async function getCodeTabs(): Promise<CodeTab[]> {
  if (_cache) return _cache;
  _cache = await Promise.all(
    snippets.map(async (snippet) => {
      const html = await codeToHtml(snippet.source, {
        lang: snippet.language,
        theme: {
          name: "backstop-dark",
          type: "dark",
          colors: {
            "editor.background": "#0D1117",
            "editor.foreground": "#E6EDF3"
          },
          tokenColors: [
            { scope: ["keyword", "storage.type"], settings: { foreground: "#FF7B72" } },
            { scope: ["string"], settings: { foreground: "#A5D6FF" } },
            { scope: ["comment"], settings: { foreground: "#8B949E", fontStyle: "italic" } },
            { scope: ["entity.name.function", "support.function"], settings: { foreground: "#FF4444" } },
            { scope: ["constant.numeric"], settings: { foreground: "#79C0FF" } },
            { scope: ["variable", "identifier"], settings: { foreground: "#E6EDF3" } }
          ]
        }
      });

      // Shiki inserts \n between each </span>\n<span class="line"> inside <pre>.
      // Those newlines are text nodes that take up a full line-height each, doubling spacing.
      const htmlClean = html.replace(/(<\/span>)\r?\n(<span class="line)/g, "$1$2");

      let lineNumber = 0;
      const htmlWithLineNumbers = htmlClean.replace(/<span class="line">/g, () => {
        lineNumber += 1;
        const isHighlighted = lineNumber === snippet.highlightLine;
        const cls = isHighlighted ? 'class="line highlighted"' : 'class="line"';
        return `<span ${cls}><span class="ln" aria-hidden="true">${lineNumber}</span>`;
      });

      return { ...snippet, html: htmlWithLineNumbers };
    })
  );
  return _cache;
}

