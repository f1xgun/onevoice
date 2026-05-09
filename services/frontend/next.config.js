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
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${process.env.API_URL || 'http://localhost:8080'}/api/v1/:path*`,
      },
      {
        source: '/media/:path*',
        destination: `${process.env.MINIO_URL || 'http://localhost:9000'}/${process.env.S3_BUCKET || 'onevoice'}/:path*`,
      },
    ];
  },
};

module.exports = withNextIntl(nextConfig);
