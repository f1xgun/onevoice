// Phase 22-02 — Filesystem loader for content/legal/*.md (D-07, D-08).
//
// Reads the per-locale Markdown file, parses gray-matter frontmatter
// (version, effective_from, title, showsController), and computes a
// SHA-256 over the body content. The hash is what the user's consent
// row pins to: server stores the same hash on POST /auth/register and
// POST /auth/consents (Plan 22-01) so we can prove which exact text
// the user accepted, not just which version string.
//
// Server Components only (uses node:fs + node:crypto). Called from the
// async page components at /legal/{slug} render time.

import { promises as fs } from 'fs';
import path from 'path';
import { createHash } from 'crypto';
import matter from 'gray-matter';

export type LegalSlug = 'privacy' | 'terms' | 'consent';

export interface LegalDoc {
  slug: LegalSlug;
  version: string;
  effectiveFrom: string;
  title: string;
  showsController: boolean;
  bodyMarkdown: string;
  sha256: string;
}

export async function loadLegalDoc(slug: LegalSlug, locale: 'ru' | 'en'): Promise<LegalDoc> {
  const filePath = path.join(process.cwd(), 'content', 'legal', `${slug}.${locale}.md`);
  const raw = await fs.readFile(filePath, 'utf8');
  const { data, content } = matter(raw);
  const sha256 = createHash('sha256').update(content).digest('hex');
  return {
    slug,
    version: String(data.version ?? ''),
    effectiveFrom: String(data.effective_from ?? ''),
    title: String(data.title ?? ''),
    showsController: Boolean(data.showsController),
    bodyMarkdown: content,
    sha256,
  };
}
