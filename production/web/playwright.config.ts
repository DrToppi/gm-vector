import { defineConfig } from "@playwright/test";
import fs from "node:fs";
import { loadFixture, validateHost } from "./browser/contracts";
const { directory } = loadFixture();
validateHost(
  process.platform,
  process.arch,
  fs.readFileSync("/etc/os-release", "utf8"),
  process.versions.node,
);
export default defineConfig({
  testDir: "./browser",
  testMatch: "critical.spec.ts",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 150000,
  globalTimeout: 180000,
  forbidOnly: true,
  expect: { timeout: 5000 },
  outputDir: directory + "/output",
  reporter: [["./browser/redacted-reporter.ts"]],
  use: {
    headless: true,
    viewport: { width: 1440, height: 1000 },
    actionTimeout: 10000,
    navigationTimeout: 15000,
    screenshot: "off",
    video: "off",
    trace: "off",
  },
  projects: [
    {
      name: "chromium",
      use: {
        browserName: "chromium",
        launchOptions: { timeout: 15000, chromiumSandbox: true },
      },
    },
    {
      name: "firefox",
      use: { browserName: "firefox", launchOptions: { timeout: 15000 } },
    },
    {
      name: "webkit",
      use: { browserName: "webkit", launchOptions: { timeout: 15000 } },
    },
  ],
});
