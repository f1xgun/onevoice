import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';

import { Checkbox } from '@/components/ui/checkbox';

// Phase 5 / Plan 05-05 — extended Checkbox primitive renders BOTH a Check glyph
// (on `data-state=checked`) AND a Minus glyph (on `data-state=indeterminate`).
//
// The actual glyph swap is a Tailwind concern not testable in jsdom, so these
// tests assert the data-state contract — that's the wire between Radix and the
// Tailwind variants `group-data-[state=*]/cb:*`.
//
// PermissionTree (D-12) depends on this contract: a partially-selected group
// renders <Checkbox checked="indeterminate" />, which MUST surface as
// `aria-checked="mixed"` (Radix sets this automatically).
describe('Checkbox (Phase 5 indeterminate glyph)', () => {
  it('exposes data-state="checked" when checked={true}', () => {
    const { container } = render(<Checkbox checked />);
    expect(container.querySelector('[data-state="checked"]')).not.toBeNull();
  });

  it('exposes data-state="indeterminate" + aria-checked="mixed" when checked="indeterminate"', () => {
    const { container } = render(<Checkbox checked="indeterminate" />);
    expect(container.querySelector('[data-state="indeterminate"]')).not.toBeNull();
    // Radix automatically maps the tri-state to ARIA — this is the a11y
    // contract the role editor relies on for screen-reader users.
    expect(container.querySelector('[aria-checked="mixed"]')).not.toBeNull();
  });

  it('exposes data-state="unchecked" when checked={false}', () => {
    const { container } = render(<Checkbox checked={false} />);
    expect(container.querySelector('[data-state="unchecked"]')).not.toBeNull();
  });

  it('renders BOTH glyph svgs inside the indicator so Tailwind toggles visibility', () => {
    // When checked="indeterminate" Radix mounts the Indicator. Both SVGs
    // must be present (Tailwind classes hide/show via group-data-[state=*]).
    // This guards against a regression where someone replaces the dual-glyph
    // pattern with a single conditional render.
    const { container } = render(<Checkbox checked="indeterminate" />);
    const svgs = container.querySelectorAll('svg');
    expect(svgs.length).toBeGreaterThanOrEqual(2);
  });
});
