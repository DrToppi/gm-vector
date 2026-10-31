import {
  test as base,
  expect,
  type Browser,
  type BrowserContext,
  type Page,
  type TestInfo,
} from "@playwright/test";
import {
  createHmac,
  privateDecrypt,
  createDecipheriv,
  constants,
  generateKeyPairSync,
} from "node:crypto";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import {
  loadFixture,
  proveServer,
  scenarios,
  validateFixture,
  validatePackages,
  versions,
  type Fixture,
  type Scenario,
} from "./contracts";
export { expect };
export const test = base.extend<{ fixture: Fixture }>({
  fixture: async ({}, provide, info) => {
    const { fixture } = loadFixture();
    if (info.project.name !== fixture.engine) throw Error("PW_ENGINE_IDENTITY");
    const readPackage = (file: string) =>
      JSON.parse(
        fs.readFileSync(path.join(process.cwd(), "node_modules", file), "utf8"),
      );
    validatePackages(
      ["@playwright/test", "playwright", "playwright-core"].map((name) =>
        readPackage(name + "/package.json"),
      ),
      readPackage("playwright-core/browsers.json"),
    );
    await proveServer(fixture);
    await provide(fixture);
  },
  context: async ({ browser, fixture }, provide) => {
    const context = await ownedContext(browser, fixture);
    await provide(context);
    await context.close();
  },
  page: async ({ context, browser, fixture }, provide, info) => {
    expect(browser.version()).toBe(versions[fixture.engine]);
    info.annotations.push({
      type: "pw-browser-version",
      description: browser.version(),
    });
    const page = await context.newPage();
    const errors: string[] = [];
    page.on("pageerror", () => {
      if (errors.length < 10) errors.push("Error");
    });
    await provide(page);
    await page.close();
    expect(errors).toEqual([]);
  },
});
export async function ownedContext(
  browser: Browser,
  fixture: Fixture,
): Promise<BrowserContext> {
  await proveServer(fixture);
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 1440, height: 1000 },
  });
  await context.addInitScript(() =>
    localStorage.setItem("gm-vector.locale", "ru"),
  );
  await context.route("**/*", (route) => {
    const url = new URL(route.request().url());
    return url.origin === fixture.origin
      ? route.continue()
      : route.abort("blockedbyclient");
  });
  return context;
}
export async function step(
  info: TestInfo,
  id: Scenario,
  action: () => Promise<void>,
) {
  await test.step(id, action);
  info.annotations.push({ type: "pw-scenario", description: id });
}
export const click = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true }).first().click();
export const field = (page: Page, selector: string, value: string) =>
  page.locator(selector).first().fill(value);
export async function api(
  page: Page,
  suffix: string,
  method = "GET",
  data?: unknown,
): Promise<any> {
  return page.evaluate(
    async ({ suffix, method, data }) => {
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
      };
      if (method !== "GET") {
        const me = await (await fetch("/api/v1/auth/me")).json();
        headers["X-CSRF-Token"] = me.csrf;
      }
      const response = await fetch("/api/v1" + suffix, {
        method,
        headers,
        ...(data === undefined ? {} : { body: JSON.stringify(data) }),
      });
      if (!response.ok) throw Error("PW_API_" + response.status);
      return response.json();
    },
    { suffix, method, data },
  );
}
export const payload = (project: any) => {
  const { loads, warnings, provisional_years, calculation_state, ...facts } =
    project;
  return facts;
};
export function otp(
  secret: string,
  step = Math.floor(Date.now() / 30000),
): string {
  const bits = [...secret]
    .map((c) =>
      "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
        .indexOf(c)
        .toString(2)
        .padStart(5, "0"),
    )
    .join("");
  const bytes = Buffer.from(bits.match(/.{8}/g)!.map((v) => parseInt(v, 2))),
    counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const h = createHmac("sha1", bytes).update(counter).digest(),
    offset = h[h.length - 1] & 15;
  return String((h.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(
    6,
    "0",
  );
}
export async function enroll(
  page: Page,
  password: string,
  name: string,
): Promise<{ secret: string; recovery: string[] }> {
  await field(page, ".auth-card input:not([autocomplete])", name);
  await field(page, 'input[autocomplete="new-password"]', password);
  await click(page, "Настроить MFA");
  await expect(page.locator(".totp-qr svg")).toBeVisible();
  const secret = (await page.locator("code.secret").textContent())!;
  expect(secret).toMatch(/^[A-Z2-7]+$/);
  // Use the already-supported previous step, then a fresh current step for
  // password login. Never reset the server replay counter or extend a TTL.
  if (Date.now() % 30000 > 28000)
    await new Promise((r) => setTimeout(r, 30010 - (Date.now() % 30000)));
  await field(
    page,
    'input[autocomplete="one-time-code"]',
    otp(secret, Math.floor(Date.now() / 30000) - 1),
  );
  await click(page, "Подтвердить");
  await expect(page.locator(".auth-card pre")).toBeVisible();
  const recovery = (await page.locator(".auth-card pre").textContent())!
    .trim()
    .split(/\s+/);
  expect(recovery).toHaveLength(10);
  await click(page, "Коды сохранены — открыть планировщик");
  await expect(page.locator(".identity")).toBeVisible();
  return { secret, recovery };
}
export async function login(
  page: Page,
  username: string,
  password: string,
  code: string,
) {
  await field(page, 'input[autocomplete="username"]', username);
  await field(page, 'input[autocomplete="current-password"]', password);
  await field(page, 'input[autocomplete="one-time-code"]', code);
  await click(page, "Войти");
  await expect(page.locator(".identity")).toBeVisible();
}
export async function apply(page: Page) {
  await click(page, "Проверить и сохранить");
  await expect(page.locator("dialog h2")).toHaveText("Проверка изменений");
  await click(page, "Применить изменения");
  await expect(page.locator("dialog")).toHaveCount(0);
}
export async function selectProject(page: Page, title: string) {
  await page.locator("nav button").filter({ hasText: title }).first().click();
  await expect(page.locator(".project-header h1")).toHaveText(title);
}
export async function downloaded(
  page: Page,
  action: () => Promise<unknown>,
): Promise<string> {
  const pending = page.waitForEvent("download");
  await action();
  const download = await pending;
  const stream = await download.createReadStream();
  if (!stream) throw Error("PW_DOWNLOAD_MISSING");
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of stream) {
    size += chunk.length;
    if (size > 64 * 1024 * 1024) throw Error("PW_DOWNLOAD_LIMIT");
    chunks.push(Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}
export async function installGate(page: Page, suffix: string, method = "GET") {
  await page.evaluate(
    ({ suffix, method }) => {
      const w = window as any,
        real = w.fetch.bind(w);
      let release!: () => void;
      const gate = new Promise<void>((r) => {
        release = r;
      });
      w.__pwGate = { ready: false, release };
      let used = false;
      w.fetch = async (input: RequestInfo | URL, options: RequestInit = {}) => {
        const { signal, ...rest } = options;
        const response = await real(input, rest);
        if (
          !used &&
          String(input).endsWith("/api/v1" + suffix) &&
          (options.method || "GET") === method
        ) {
          used = true;
          const json = response.json.bind(response);
          response.json = async () => {
            const result = await json();
            w.__pwGate.ready = true;
            await gate;
            return result;
          };
        }
        return response;
      };
      w.__pwRestore = () => {
        w.fetch = real;
      };
    },
    { suffix, method },
  );
}
export async function releaseGate(page: Page) {
  await page.evaluate(() => {
    const w = window as any;
    w.__pwGate.release();
    w.__pwRestore();
  });
  await page.evaluate(
    () =>
      new Promise<void>((r) =>
        requestAnimationFrame(() => requestAnimationFrame(() => r())),
      ),
  );
}
export async function nativeInteractions(
  page: Page,
  project: string,
  sampleTask: any,
) {
  const require = createRequire(import.meta.url);
  const native = require("../../scripts/wp03-browser-interactions.cjs");
  const evaluate = (expression: string) => page.evaluate(expression);
  const send = async (method: string, p: any) => {
    if (method === "Input.dispatchMouseEvent") {
      await page.mouse.move(p.x, p.y);
      if (p.type === "mousePressed")
        await page.mouse.down({
          button: p.button || "left",
          clickCount: p.clickCount || 1,
        });
      else if (p.type === "mouseReleased")
        await page.mouse.up({
          button: p.button || "left",
          clickCount: p.clickCount || 1,
        });
      else if (p.type === "mouseWheel")
        await page.mouse.wheel(p.deltaX, p.deltaY);
    } else if (method === "Input.dispatchKeyEvent") {
      if (p.type === "keyDown") {
        if (p.modifiers & 8) await page.keyboard.down("Shift");
        await page.keyboard.down(p.key);
      } else {
        await page.keyboard.up(p.key);
        if (p.modifiers & 8) await page.keyboard.up("Shift");
      }
    } else throw Error("PW_NATIVE_METHOD");
  };
  await native({
    send,
    evaluate,
    wait: (expression: string) =>
      page.waitForFunction(expression, undefined, { timeout: 5000 }),
    read: (suffix: string) => api(page, suffix),
    click: (name: string) => click(page, name),
    field: (selector: string, value: string) => field(page, selector, value),
    reload: async (expression: string) => {
      await page.reload();
      await page.waitForFunction(expression);
    },
    originalProject: project,
    sampleTask,
    setPhase: () => {},
  });
}
export function validateNegativeContracts(f: Fixture) {
  for (const mutate of [
    (x: any) => {
      x.origin = "https://example.com";
    },
    (x: any) => {
      x.engine = "missing";
    },
    (x: any) => {
      delete x.nonce;
    },
    (x: any) => {
      x.extra = true;
    },
  ]) {
    const value = structuredClone(f);
    mutate(value);
    expect(() => validateFixture(value)).toThrow();
  }
  expect(scenarios).toHaveLength(9);
}

export function decryptBug(raw: string, privateKey: string) {
  const parts = raw.split(".");
  expect(parts).toHaveLength(5);
  const header = JSON.parse(Buffer.from(parts[0], "base64url").toString());
  expect(header.alg).toBe("RSA-OAEP-256");
  expect(header.enc).toBe("A256GCM");
  const key = privateDecrypt(
    {
      key: privateKey,
      padding: constants.RSA_PKCS1_OAEP_PADDING,
      oaepHash: "sha256",
    },
    Buffer.from(parts[1], "base64url"),
  );
  const decipher = createDecipheriv(
    "aes-256-gcm",
    key,
    Buffer.from(parts[2], "base64url"),
  );
  decipher.setAAD(Buffer.from(parts[0]));
  decipher.setAuthTag(Buffer.from(parts[4], "base64url"));
  return JSON.parse(
    Buffer.concat([
      decipher.update(Buffer.from(parts[3], "base64url")),
      decipher.final(),
    ]).toString(),
  );
}
export function wrongReportKey() {
  return generateKeyPairSync("rsa", { modulusLength: 2048 })
    .privateKey.export({ format: "pem", type: "pkcs8" })
    .toString();
}
