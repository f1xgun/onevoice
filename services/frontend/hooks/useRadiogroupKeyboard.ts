'use client';

import { useCallback, useRef } from 'react';
import type { KeyboardEvent } from 'react';

/**
 * Keyboard contract for `role="radiogroup"` containers built from
 * `<button role="radio">` elements (instead of native `<input type="radio">`).
 *
 * Native `<button>` does NOT get arrow-key navigation between sibling radios
 * — that's only wired by the user agent for actual `<input type="radio">`.
 * When axe-core / NVDA / JAWS announce "use arrows to choose", the keys must
 * actually do something. This hook is the small piece of glue that makes
 * them do something.
 *
 * Contract (WAI-ARIA Authoring Practices §radiogroup):
 *   - Only ONE radio is in the tab order at a time. Use `getTabIndex(value)`
 *     on each radio: returns 0 for the checked one, -1 for the rest. Tab
 *     into the group lands on the checked radio.
 *   - ArrowRight / ArrowDown → move to next radio, set it checked, focus it.
 *   - ArrowLeft / ArrowUp → move to previous, same behavior.
 *   - Home → first radio, set checked, focus it.
 *   - End → last radio, set checked, focus it.
 *   - Activating moves the group's value AND focus together — radiogroup
 *     semantics have no separate "focused-but-not-selected" state.
 *
 * Precedent: `components/ui/approval-switch.tsx` implements the same
 * pattern inline using individual refs. We avoid threading per-button refs
 * here by selecting via `data-radiogroup-value="<v>"`, which keeps the
 * caller's JSX flat — the consumer just spreads `getRadioProps(value)` on
 * each button.
 *
 * Usage:
 * ```tsx
 * const { onKeyDown, getRadioProps } = useRadiogroupKeyboard({
 *   options: ['all', 'published', 'scheduled'] as const,
 *   value: filters.status,
 *   onValueChange: (v) => setFilter('status', v),
 * });
 * return (
 *   <div role="radiogroup" onKeyDown={onKeyDown}>
 *     {options.map((v) => (
 *       <button key={v} role="radio" aria-checked={value === v} {...getRadioProps(v)}>
 *         {label(v)}
 *       </button>
 *     ))}
 *   </div>
 * );
 * ```
 */
export interface UseRadiogroupKeyboardOptions<T extends string> {
  options: readonly T[];
  value: T;
  onValueChange: (value: T) => void;
}

export interface UseRadiogroupKeyboardResult<T extends string> {
  /** Attach to the `role="radiogroup"` container's onKeyDown. */
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  /**
   * Spread on each `<button role="radio">`. Wires `tabIndex` (0 for the
   * checked option, -1 otherwise) and `data-radiogroup-value` so the
   * container's keydown handler can find the next button via querySelector.
   */
  getRadioProps: (v: T) => {
    tabIndex: 0 | -1;
    'data-radiogroup-value': T;
  };
}

export function useRadiogroupKeyboard<T extends string>({
  options,
  value,
  onValueChange,
}: UseRadiogroupKeyboardOptions<T>): UseRadiogroupKeyboardResult<T> {
  const stateRef = useRef({ options, value });
  stateRef.current = { options, value };

  const onKeyDown = useCallback(
    (e: KeyboardEvent<HTMLElement>) => {
      const { options: opts, value: current } = stateRef.current;
      if (opts.length === 0) return;
      const idx = opts.indexOf(current);
      if (idx < 0) return;

      let nextIdx: number | null = null;
      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
        nextIdx = (idx + 1) % opts.length;
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        nextIdx = (idx - 1 + opts.length) % opts.length;
      } else if (e.key === 'Home') {
        nextIdx = 0;
      } else if (e.key === 'End') {
        nextIdx = opts.length - 1;
      }

      if (nextIdx === null) return;
      e.preventDefault();
      const nextValue = opts[nextIdx];
      onValueChange(nextValue);

      const container = e.currentTarget;
      const focusNext = () => {
        const target = container.querySelector<HTMLElement>(
          `[data-radiogroup-value="${CSS.escape(nextValue)}"]`
        );
        target?.focus();
      };
      if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
        window.requestAnimationFrame(focusNext);
      } else {
        focusNext();
      }
    },
    [onValueChange]
  );

  const getRadioProps = useCallback(
    (v: T) => ({
      tabIndex: (v === value ? 0 : -1) as 0 | -1,
      'data-radiogroup-value': v,
    }),
    [value]
  );

  return { onKeyDown, getRadioProps };
}
