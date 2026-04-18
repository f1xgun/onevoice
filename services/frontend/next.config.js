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

module.exports = nextConfig;
