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
      renderWidget();
      return;
    }

    window.__onSmartCaptchaLoad = renderWidget;
    const script = document.createElement('script');
    script.src =
      'https://smartcaptcha.yandexcloud.net/captcha.js?render=onload&onload=__onSmartCaptchaLoad';
    script.async = true;
    document.head.appendChild(script);
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
        }),
    }),
    []
  );

  return <div ref={containerRef} aria-hidden="true" />;
});
