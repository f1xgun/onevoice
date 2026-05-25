// Phase 22-02 — Surface D helper: parses inline `[label](href)` markdown
// inside a checkbox label and renders anchors with target="_blank" +
// rel="noopener noreferrer" so the user can read the linked policy in a
// new tab without losing unsubmitted Register form state (UI-SPEC §D
// "Each link MUST have target='_blank'"). The aria-label appends
// "(откроется в новой вкладке)" so screen readers announce the new-tab
// behaviour.

import type { ReactNode } from 'react';

const LINK_RE = /\[([^\]]+)\]\(([^)]+)\)/g;

export function ConsentCheckboxLabel({ text }: { text: string }) {
  const out: ReactNode[] = [];
  let lastIndex = 0;
  let i = 0;
  // RegExp.exec maintains state via lastIndex when the regex has the /g
  // flag — re-creating the iterator each call would loop forever.
  let m: RegExpExecArray | null;
  const re = new RegExp(LINK_RE.source, 'g');
  while ((m = re.exec(text)) !== null) {
    if (m.index > lastIndex) {
      out.push(text.slice(lastIndex, m.index));
    }
    const label = m[1];
    const href = m[2];
    out.push(
      <a
        key={`l${i++}`}
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={`${label} (откроется в новой вкладке)`}
        className="text-[var(--ov-accent)] hover:underline"
      >
        {label}
      </a>
    );
    lastIndex = m.index + m[0].length;
  }
  if (lastIndex < text.length) {
    out.push(text.slice(lastIndex));
  }
  return <>{out}</>;
}
