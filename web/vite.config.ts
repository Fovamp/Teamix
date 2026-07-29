import { defineConfig } from "vite"
import vue from "@vitejs/plugin-vue"

export default defineConfig({
  plugins: [vue()],
  base: "/v3/",
  build: {
    outDir: "dist-v3",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/events": "http://localhost:8787",
      "/submit": "http://localhost:8787",
      "/teamix": "http://localhost:8787",
      "/status": "http://localhost:8787",
      "/history": "http://localhost:8787",
      "/sessions": "http://localhost:8787",
      "/models": "http://localhost:8787",
      "/checkpoints": "http://localhost:8787",
      "/branches": "http://localhost:8787",
      "/cancel": "http://localhost:8787",
      "/compact": "http://localhost:8787",
      "/new": "http://localhost:8787",
      "/plan": "http://localhost:8787",
      "/rewind": "http://localhost:8787",
      "/fork": "http://localhost:8787",
      "/delete-session": "http://localhost:8787",
    },
  },
})
