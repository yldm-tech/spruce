import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite-plus";

export default defineConfig({
  // The router plugin must run before the React plugin: it generates src/routeTree.gen.ts from src/routes and splits each route into its own chunk.
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react()],
  server: {
    // The development app loads this address (devUrl in mygo.config.ts).
    port: 5173,
    strictPort: true,
    // The development app bundle and the packaged builds are not the page's.
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/test/setup.ts"],
  },
  lint: {
    plugins: ["eslint", "typescript", "unicorn", "oxc", "react", "jsx-a11y", "import", "vitest"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    categories: {
      correctness: "error",
    },
    rules: {
      "eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_" },
      ],
      "typescript/no-explicit-any": "error",
      "react/rules-of-hooks": "error",
      "react/exhaustive-deps": "warn",
      "vite-plus/prefer-vite-plus-imports": "error",
      // A disabled link is a span with role="link" and aria-disabled (WAI-ARIA), which an <a> without href cannot express; the rule would demand the tag that anchor-is-valid then rejects.
      "jsx-a11y/prefer-tag-over-role": "off",
    },
    ignorePatterns: ["dist/**", "build/**", ".mygo/**", "src/routeTree.gen.ts", "src/mygo.ts"],
    options: {
      typeAware: true,
      typeCheck: true,
    },
  },
  fmt: {
    printWidth: 110,
    trailingComma: "all",
    ignorePatterns: [
      "dist/**",
      "build/**",
      ".mygo/**",
      ".agents/**",
      "src/routeTree.gen.ts",
      "src/mygo.ts",
      "internal/**",
      "bun.lock",
    ],
  },
});
