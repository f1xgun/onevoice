'use client';

import { useEffect } from 'react';

/**
 * Prompts the user before leaving the page (browser close / reload / external
 * nav) when `isDirty` is true.
 *
 * Internal Next.js App Router navigations are NOT intercepted (App Router
 * exposes no route-change-start event in v14; the router.push() in
 * RoleEditorForm's submit handler therefore navigates silently — that's the
 * correct behavior for a successful save). Internal-nav guard for cancel /
 * back-link / sidebar clicks is deferred to v2.1 per Plan 05-07 CONTEXT
 * Claude's discretion.
 *
 * Modern Chrome / Firefox / Safari ignore the custom `message` and show their
 * own generic «Покинуть сайт?» / «Leave site?» dialog — that's by design to
 * prevent abuse. We preserve the `message` parameter for legacy browser
 * support and accessibility tooling.
 *
 * SSR-safe: `window` access guarded by `typeof window`.
 */
export function useUnsavedChangesPrompt(isDirty: boolean, message: string): void {
  useEffect(() => {
    if (!isDirty || typeof window === 'undefined') return;
    function handler(event: BeforeUnloadEvent) {
      event.preventDefault();
      // Legacy browsers read `returnValue`; modern browsers ignore the
      // string but still require it (or preventDefault) to actually show
      // the prompt.
      event.returnValue = message;
      return message;
    }
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [isDirty, message]);
}
