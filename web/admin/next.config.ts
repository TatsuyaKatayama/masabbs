import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'standalone',
  async rewrites() {
    const apiUrl = process.env.API_PROXY_URL || 'http://localhost:8080';

    return [
      {
        source: '/api/:path*',
        destination: `${apiUrl}/api/:path*`,
      },
      {
        source: '/ws',
        destination: `${apiUrl}/ws`,
      },
    ];
  },
};

export default nextConfig;
