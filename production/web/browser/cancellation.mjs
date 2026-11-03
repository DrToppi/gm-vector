import fs from "node:fs";
import path from "node:path";
import { test, expect } from "./fixtures.ts";
import { loadFixture, revisions } from "./contracts.ts";

test("owned browser cancellation lifecycle", async ({
  page,
  fixture,
  browser,
}) => {
  if (process.env.GMA_PW_LIFECYCLE_PROBE !== "1")
    throw Error("PW_LIFECYCLE_MODE_REQUIRED");
  await page.goto(fixture.origin);
  await expect(page).toHaveTitle("gmVector");
  const { directory } = loadFixture();
  const temporary = path.join(directory, "cancellation-ready.partial.json");
  fs.writeFileSync(
    temporary,
    JSON.stringify({
      format: "gm-vector-browser-ready",
      schema_version: 1,
      run_nonce: fixture.nonce,
      engine: fixture.engine,
      playwright_version: "1.64.1",
      browser_revision: revisions[fixture.engine],
      browser_version: browser.version(),
      worker_pid: process.pid,
      stage: "owned-page-created",
    }),
    { flag: "wx", mode: 0o600 },
  );
  fs.linkSync(temporary, path.join(directory, "cancellation-ready.json"));
  fs.unlinkSync(temporary);
  // The Go owner requests cancellation only after validated readiness and
  // nonempty ownership observation. Its absolute15s deadline bounds this wait.
  await new Promise(() => {});
});
