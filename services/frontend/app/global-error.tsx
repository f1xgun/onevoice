'use client';

/* eslint-disable i18next/no-literal-string --
   global-error replaces the ROOT layout, so NextIntlClientProvider and
   globals.css are both unavailable here. This is the last-resort fallback for
   a crash in the root layout itself; copy is a fixed neutral string and styles
   are inline so the panel renders without the design system. */

export default function GlobalError({
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="ru">
      <body
        style={{
          margin: 0,
          minHeight: '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: '#f5f3ee',
          color: '#2b2a27',
          fontFamily: 'system-ui, -apple-system, Segoe UI, Roboto, sans-serif',
          padding: '2rem',
        }}
      >
        <div style={{ maxWidth: 420, textAlign: 'center' }}>
          <h1 style={{ fontSize: 22, fontWeight: 500, margin: '0 0 8px' }}>Что-то пошло не так</h1>
          <p style={{ fontSize: 14, lineHeight: 1.6, color: '#6b6862', margin: '0 0 20px' }}>
            Приложение столкнулось с непредвиденной ошибкой. Попробуйте перезагрузить страницу.
          </p>
          <button
            type="button"
            onClick={reset}
            style={{
              border: '1px solid #2b2a27',
              backgroundColor: '#2b2a27',
              color: '#fff',
              borderRadius: 8,
              padding: '10px 20px',
              fontSize: 14,
              cursor: 'pointer',
            }}
          >
            Перезагрузить
          </button>
        </div>
      </body>
    </html>
  );
}
