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
        source: '/chat/:path*',
        destination: `${process.env.ORCHESTRATOR_URL || 'http://localhost:8090'}/chat/:path*`,
      },
    ];
  },
};

module.exports = nextConfig;
