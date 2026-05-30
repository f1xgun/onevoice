'use client';

// Yandex SmartCaptcha invisible-mode widget.
//
// Loaded only when the backend signals captcha_required (HTTP 400 with
// {code: "captcha_required"}). Renders an invisible <div>, injects the
// Yandex captcha.js (one-shot, idempotent across re-mounts), and exposes
// an imperative `execute()` that resolves with the user-issued token.
//
// The token is sent on the *next* /auth/login POST as the X-Captcha-Token
// header. The backend forwards it to https://smartcaptcha.yandexcloud.net/validate
// for server-side validation.
//
// Reference: https://yandex.cloud/en/docs/smartcaptcha/concepts/widget-methods

import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react';

declare global {
  interface Window {
    smartCaptcha?: {
      render: (
        container: HTMLElement,
        params: {
          sitekey: string;
          invisible: boolean;
          callback?: (token: string) => void;
        }
      ) => number;
      execute: (widgetId?: number) => void;
      reset: (widgetId?: number) => void;
      getResponse: (widgetId?: number) => string;
    };
    __onSmartCaptchaLoad?: () => void;
  }
}

export interface SmartCaptchaHandle {
  /**
   * Triggers the invisible challenge and resolves with the user-issued
   * token. Calling before the widget script has finished loading parks
   * the resolver until the widget is ready; calling twice in rapid
   * succession parks both — Yandex only delivers one callback per
   * execute(), so the second resolver is discarded (a `reset` would be
   * needed for a second challenge; not implemented in v1 — we re-mount).
   */
  execute: () => Promise<string>;
}

interface Props {
  siteKey: string;
}

export const SmartCaptcha = forwardRef<SmartCaptchaHandle, Props>(function SmartCaptcha(
  { siteKey },
  ref
) {
  const containerRef = useRef<HTMLDivElement>(null);
  const widgetIdRef = useRef<number | null>(null);
  const tokenResolveRef = useRef<((t: string) => void) | null>(null);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    const renderWidget = () => {
      if (!containerRef.current || !window.smartCaptcha) return;
      widgetIdRef.current = window.smartCaptcha.render(containerRef.current, {
        sitekey: siteKey,
        invisible: true,
        callback: (token: string) => {
          tokenResolveRef.current?.(token);
          tokenResolveRef.current = null;
        },
      });
    };

    if (window.smartCaptcha) {
      // Script already loaded by a previous mount — render directly.
      renderWidget();
      return;
    }

    // First mount in this page lifetime. Inject the script and let
    // Yandex call our onload hook. The script is idempotent so a
    // duplicate inject (e.g. React StrictMode double-effect in dev) is
    // a noop on the production runtime.
    window.__onSmartCaptchaLoad = renderWidget;
    const script = document.createElement('script');
    script.src =
      'https://smartcaptcha.yandexcloud.net/captcha.js?render=onload&onload=__onSmartCaptchaLoad';
    script.async = true;
    document.head.appendChild(script);
    // No cleanup of widgetId — Yandex widget owns the container DOM and
    // does not expose a destroy(). On unmount the container disappears
    // with the React subtree.
  }, [siteKey]);

  useImperativeHandle(
    ref,
    () => ({
      execute: () =>
        new Promise<string>((resolve) => {
          tokenResolveRef.current = resolve;
          if (widgetIdRef.current !== null && window.smartCaptcha) {
            window.smartCaptcha.execute(widgetIdRef.current);
          }
          // If the widget hasn't rendered yet, the resolver waits — the
          // callback installed in renderWidget will pick it up when the
          // user solves the challenge.
        }),
    }),
    []
  );

  return <div ref={containerRef} aria-hidden="true" />;
});
