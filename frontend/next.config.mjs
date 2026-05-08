import { createMDX } from "fumadocs-mdx/next";

/** @type {import('next').NextConfig} */
const nextConfig = {
  compress: true,
  poweredByHeader: false,
  images: {
    remotePatterns: [
      {
        protocol: "https",
        hostname: "avatars.githubusercontent.com"
      }
    ],
    formats: ["image/avif", "image/webp"]
  },
  experimental: {
    optimizePackageImports: [
      "@tabler/icons-react",
      "framer-motion",
      "simple-icons",
      "animejs",
      "date-fns"
    ]
  },
  webpack(config) {
    config.watchOptions = {
      ...(config.watchOptions ?? {}),
      ignored:
        /[\\/](node_modules|\.next|\.git)[\\/]|^C:[\\/](DumpStack\.log\.tmp|pagefile\.sys|swapfile\.sys)$|^C:[\\/]System Volume Information(?:[\\/]|$)/i
    };
    return config;
  }
};

export default createMDX()(nextConfig);
