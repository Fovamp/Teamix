import { defineConfig } from "vite"
import vue from "@vitejs/plugin-vue"

export default defineConfig({
  plugins: [vue()],
  base: "/v3/",
  build: {
    // 直接输出到 Go embed 目录，省去手动同步
    outDir: "../internal/serve/webdist-v3",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // Proxy ALL non-/v3/ requests to Teamix Go backend
      "/": {
        target: "http://localhost:8787",
        bypass: (req) => {
          if (req.url?.startsWith("/v3/")) return req.url
        },
      },
    },
  },
})
