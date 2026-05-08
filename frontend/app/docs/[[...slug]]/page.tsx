import { notFound, redirect } from "next/navigation";
import { source } from "@/source";
import { getMDXComponents } from "@/components/docs/mdx-components";
import { DocsBreadcrumb } from "@/components/docs/breadcrumb";
import { DocsTOC } from "@/components/docs/toc";
import { DocsPageNav } from "@/components/docs/page-nav";
import type { Metadata } from "next";

interface PageProps {
  params: Promise<{ slug?: string[] }>;
}

export async function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug } = await params;
  const page = source.getPage(slug);
  if (!page) return {};
  return {
    title: `${page.data.title} — Backstop Docs`,
    description: page.data.description,
  };
}

export default async function DocPage({ params }: PageProps) {
  const { slug } = await params;
  if (!slug || slug.length === 0) redirect("/docs/getting-started/introduction");
  const page = source.getPage(slug);
  if (!page) notFound();

  const MDX = page.data.body;
  const toc = page.data.toc ?? [];
  const components = getMDXComponents({});

  return (
    <div className="flex w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 lg:py-16 gap-12">
      {/* Main content + TOC side-by-side */}
      <article className="flex-1 min-w-0 max-w-[800px] mx-auto lg:mx-0">
        <div className="mb-6">
          <DocsBreadcrumb page={page} tree={source.pageTree} />
        </div>

        <header className="mb-10 pb-8 border-b border-white/10">
          <h1 className="font-sans text-3xl sm:text-4xl font-bold tracking-tight text-white mb-4">
            {page.data.title}
          </h1>
          {page.data.description && (
            <p className="text-lg text-white/60 leading-relaxed max-w-2xl">
              {page.data.description}
            </p>
          )}
        </header>

        <div className="prose prose-invert prose-p:leading-loose prose-pre:bg-transparent prose-pre:p-0 max-w-none">
          <MDX components={components} />
        </div>

        <div className="mt-16 pt-8 border-t border-white/10">
          <DocsPageNav page={page} tree={source.pageTree} />
        </div>
      </article>

      {/* TOC — hidden on mobile, right rail on desktop */}
      {toc.length > 0 && (
        <aside className="hidden xl:block w-[240px] shrink-0">
          <div className="sticky top-[100px]">
            <DocsTOC items={toc} />
          </div>
        </aside>
      )}
    </div>
  );
}
