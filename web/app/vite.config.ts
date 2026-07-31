import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // A GitHub Pages project site serves from a sub-path, and the deploy
  // workflow sets this. Everything else, dev included, stays at the
  // root, so the smoke and visual gates are unaffected.
  base: process.env["VITE_BASE"] ?? "/",
  plugins: [react()],
  test: {
    environment: "jsdom",
    // globals lets testing-library register its automatic cleanup.
    globals: true,
    setupFiles: ["tests/setup.ts"],
    include: ["tests/**/*.test.ts", "tests/**/*.test.tsx"],
  },
});
