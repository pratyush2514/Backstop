import Link from "next/link";
import { getBreadcrumbItems } from "fumadocs-core/breadcrumb";
import type { Root } from "fumadocs-core/page-tree";

interface BreadcrumbProps {
  page: ReturnType<typeof import("@/source").source.getPage>;
  tree: Root;
}

export function DocsBreadcrumb({ page, tree }: BreadcrumbProps) {
  if (!page) return null;
  const breadcrumbs = getBreadcrumbItems(page.url, tree);

  if (!breadcrumbs.length) return null;

  return (
    <nav aria-label="Breadcrumb" className="docs-breadcrumb">
      {breadcrumbs.map((crumb, i) => (
        <span key={i} className="docs-breadcrumb-item">
          {i > 0 && <span className="docs-breadcrumb-sep">/</span>}
          {crumb.url ? (
            <Link href={crumb.url} className="docs-breadcrumb-link">
              {crumb.name}
            </Link>
          ) : (
            <span className="docs-breadcrumb-current">{crumb.name}</span>
          )}
        </span>
      ))}
    </nav>
  );
}
