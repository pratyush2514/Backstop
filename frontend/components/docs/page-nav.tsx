import Link from "next/link";
import { findNeighbour } from "fumadocs-core/page-tree";
import type { Root } from "fumadocs-core/page-tree";
import { IconArrowLeft, IconArrowRight } from "@tabler/icons-react";

interface PageNavProps {
  page: ReturnType<typeof import("@/source").source.getPage>;
  tree: Root;
}

export function DocsPageNav({ page, tree }: PageNavProps) {
  if (!page) return null;
  const { previous: prev, next } = findNeighbour(tree, page.url);

  if (!prev && !next) return null;

  return (
    <nav className="docs-page-nav" aria-label="Page navigation">
      {prev ? (
        <Link href={prev.url} className="docs-page-nav-item docs-page-nav-prev">
          <IconArrowLeft size={16} stroke={1.5} className="shrink-0" />
          <span>
            <span className="docs-page-nav-dir">Previous</span>
            <span className="docs-page-nav-title">{prev.name}</span>
          </span>
        </Link>
      ) : <div />}

      {next ? (
        <Link href={next.url} className="docs-page-nav-item docs-page-nav-next">
          <span>
            <span className="docs-page-nav-dir">Next</span>
            <span className="docs-page-nav-title">{next.name}</span>
          </span>
          <IconArrowRight size={16} stroke={1.5} className="shrink-0" />
        </Link>
      ) : <div />}
    </nav>
  );
}
