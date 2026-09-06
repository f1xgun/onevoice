const createNextIntlPlugin = require('next-intl/plugin');

// next-intl request config lives at lib/i18n/request.ts. The plugin wires
// it into the App Router so server components + middleware can pull
// messages without per-route plumbing. Single-locale (ru) for now —
// adding english later only requires extending lib/i18n/request.ts and
// dropping a messages/en.json next to ru.json.
const withNextIntl = createNextIntlPlugin('./lib/i18n/request.ts');

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  poweredByHeader: false,
  async headers() {
    const isDevelopment = process.env.NODE_ENV === 'development';
    const contentSecurityPolicy = [
      "default-src 'self'",
      `script-src 'self' 'unsafe-inline' https://smartcaptcha.yandexcloud.net https://smartcaptcha.cloud.yandex.ru${isDevelopment ? " 'unsafe-eval'" : ''}`,
      "style-src 'self' 'unsafe-inline'",
      "img-src 'self' data: blob: https:",
      "font-src 'self'",
      `connect-src 'self' https://smartcaptcha.yandexcloud.net https://smartcaptcha.cloud.yandex.ru${isDevelopment ? ' ws: wss:' : ''}`,
      "media-src 'self' blob:",
      "object-src 'none'",
      "base-uri 'self'",
      "form-action 'self'",
      'frame-src https://smartcaptcha.yandexcloud.net https://smartcaptcha.cloud.yandex.ru',
      "frame-ancestors 'none'",
    ].join('; ');

    return [
      {
        source: '/:path*',
        headers: [
          { key: 'Content-Security-Policy', value: contentSecurityPolicy },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          { key: 'Permissions-Policy', value: 'camera=(), microphone=(), geolocation=()' },
          { key: 'Cross-Origin-Opener-Policy', value: 'same-origin-allow-popups' },
        ],
      },
    ];
  },
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${process.env.API_URL || 'http://localhost:8080'}/api/v1/:path*`,
      },
      {
        source: '/media/:path+',
        destination: `${process.env.MINIO_URL || 'http://localhost:9000'}/${process.env.S3_BUCKET || 'onevoice'}/:path+`,
      },
    ];
  },
};

module.exports = withNextIntl(nextConfig);
