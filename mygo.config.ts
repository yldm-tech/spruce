import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Spruce",
  slug: "spruce",
  identifier: "ai.yldm.spruce",
  version: "0.1.0",
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs buildCommand and embeds frontendDist into the app. Both commands are Vite+ (vp dev / vp build) through the package.json scripts.
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
});
