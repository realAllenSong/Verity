import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  allowedDevOrigins: ["127.0.0.1"],
  experimental: {
    optimizePackageImports: ["@phosphor-icons/react", "@radix-ui/themes"],
  },
};

export default nextConfig;
