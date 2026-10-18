import { defineConfig } from '@playwright/test';

// Browser E2E runs against a real CoBot core started by globalSetup with the
// fake codex host as its engine. Everything except the model is the product.
export default defineConfig({
  testDir: './specs',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: process.env.COBOT_E2E_BASE_URL ?? 'http://127.0.0.1:18777',
    trace: 'retain-on-failure',
    // Wide enough that the team list and work panel are laid out in-flow
    // rather than overlaying the conversation (the product collapses them on
    // narrow screens by design).
    viewport: { width: 1440, height: 900 },
  },
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
});
