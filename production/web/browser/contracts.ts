import fs from "node:fs";
import path from "node:path";
import https from "node:https";
export const engines = ["chromium", "firefox", "webkit"] as const;
export type Engine = (typeof engines)[number];
export function certificateFailure(
  engine: Engine,
  message: string,
): string | null {
  const patterns: Record<Engine, RegExp> = {
    chromium:
      /\bnet::ERR_CERT_(?:AUTHORITY_INVALID|INVALID|COMMON_NAME_INVALID)\b/,
    firefox:
      /\b(?:SSL_ERROR_UNKNOWN|SEC_ERROR_UNKNOWN_ISSUER|MOZILLA_PKIX_ERROR_SELF_SIGNED_CERT|SSL_ERROR_BAD_CERT_DOMAIN)\b/,
    webkit: /\bUnacceptable TLS certificate\b/,
  };
  return patterns[engine].test(message)
    ? engine + "-certificate-rejected"
    : null;
}
export const scenarios = [
  "B01",
  "B02",
  "B03",
  "B04",
  "B05",
  "B06",
  "B07",
  "B08",
  "B09",
] as const;
export type Scenario = (typeof scenarios)[number];
export const revisions: Record<Engine, string> = {
  chromium: "1248",
  firefox: "1555",
  webkit: "2370",
};
export const versions: Record<Engine, string> = {
  chromium: "156.0.8078.4",
  firefox: "157.0",
  webkit: "27.2",
};
export function validateHost(
  platform: string,
  architecture: string,
  release: string,
  node: string,
): void {
  const properties = Object.fromEntries(
    release.split("\n").flatMap((line) => {
      const match = line.match(/^([A-Z_]+)=(?:"([^"]*)"|([^"\s]*))$/);
      return match ? [[match[1], match[2] ?? match[3]]] : [];
    }),
  );
  if (
    platform !== "linux" ||
    architecture !== "x64" ||
    properties.ID !== "ubuntu" ||
    !["24.04", "26.04"].includes(properties.VERSION_ID) ||
    !/^24\./.test(node)
  )
    throw Error("PW_UNSUPPORTED_REFERENCE_HOST");
}
export function validatePackages(packages: unknown[], manifest: unknown): void {
  if (
    packages.length !== 3 ||
    packages.some(
      (pkg) =>
        !pkg ||
        typeof pkg !== "object" ||
        (pkg as { version?: unknown }).version !== "1.64.1",
    )
  )
    throw Error("PW_PACKAGE_VERSION");
  const browsers = (manifest as { browsers?: unknown })?.browsers;
  if (!Array.isArray(browsers)) throw Error("PW_BROWSER_MANIFEST");
  for (const engine of [...engines, "chromium-headless-shell"] as const) {
    const expected = engine === "chromium-headless-shell" ? "chromium" : engine;
    const entries = browsers.filter((entry) => entry?.name === engine);
    if (
      entries.length !== 1 ||
      entries[0].revision !== revisions[expected] ||
      entries[0].browserVersion !== versions[expected]
    )
      throw Error("PW_BROWSER_REVISION");
  }
}
export type Fixture = {
  format: "gm-vector-playwright-fixture";
  schema_version: 1;
  engine: Engine;
  nonce: string;
  origin: string;
  certificate: string;
  mismatch_origin: string;
  mismatch_certificate: string;
  username: string;
  password: string;
  report_private_key: string;
  calendar_id: string;
  history_project: string;
};
export function validateFixture(value: unknown): Fixture {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("PW_IPC_SCHEMA");
  const f = value as Fixture;
  const keys = [
    "format",
    "schema_version",
    "engine",
    "nonce",
    "origin",
    "certificate",
    "mismatch_origin",
    "mismatch_certificate",
    "username",
    "password",
    "report_private_key",
    "calendar_id",
    "history_project",
  ];
  if (
    Object.keys(f).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(f, k)) ||
    f.format !== "gm-vector-playwright-fixture" ||
    f.schema_version !== 1 ||
    !engines.includes(f.engine)
  )
    throw Error("PW_IPC_SCHEMA");
  for (const key of keys.filter((k) => k !== "schema_version"))
    if (typeof (f as unknown as Record<string, unknown>)[key] !== "string")
      throw Error("PW_IPC_TYPE");
  for (const origin of [f.origin, f.mismatch_origin]) {
    const u = new URL(origin);
    if (
      u.protocol !== "https:" ||
      u.hostname !== "127.0.0.1" ||
      !u.port ||
      ["9443", "19443", "29443"].includes(u.port) ||
      u.username ||
      u.password ||
      u.pathname !== "/" ||
      u.search ||
      u.hash ||
      origin !== u.origin
    )
      throw Error("PW_IPC_ORIGIN");
  }
  if (f.origin === f.mismatch_origin) throw Error("PW_IPC_ORIGIN");
  if (
    !/^[A-Za-z0-9_-]{40,64}$/.test(f.nonce) ||
    f.password.length < 15 ||
    f.password.length > 128 ||
    !f.certificate.startsWith("-----BEGIN CERTIFICATE-----") ||
    !f.mismatch_certificate.startsWith("-----BEGIN CERTIFICATE-----") ||
    !f.report_private_key.startsWith("-----BEGIN PRIVATE KEY-----")
  )
    throw Error("PW_IPC_VALUE");
  return f;
}
export function loadFixture(): { fixture: Fixture; directory: string } {
  const file = process.env.GMA_PW_FIXTURE_FILE;
  if (!file || !path.isAbsolute(file)) throw Error("PW_IPC_REQUIRED");
  let ancestor = path.parse(file).root;
  for (const part of path
    .dirname(file)
    .slice(ancestor.length)
    .split(path.sep)) {
    ancestor = path.join(ancestor, part);
    if (fs.lstatSync(ancestor).isSymbolicLink())
      throw Error("PW_IPC_DIRECTORY");
  }
  const directory = path.dirname(file),
    parent = fs.lstatSync(directory);
  if (
    parent.isSymbolicLink() ||
    !parent.isDirectory() ||
    parent.mode & 0o077 ||
    parent.uid !== process.getuid?.()
  )
    throw Error("PW_IPC_DIRECTORY");
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const stat = fs.fstatSync(fd);
    if (
      !stat.isFile() ||
      stat.mode & 0o077 ||
      stat.uid !== parent.uid ||
      stat.size > 65536
    )
      throw Error("PW_IPC_FILE");
    const raw = fs.readFileSync(fd, "utf8");
    let value: unknown;
    try {
      value = JSON.parse(raw);
    } catch {
      throw Error("PW_IPC_JSON");
    }
    if (JSON.stringify(value) !== raw) throw Error("PW_IPC_CANONICAL");
    return { fixture: validateFixture(value), directory };
  } finally {
    fs.closeSync(fd);
  }
}
export async function proveServer(f: Fixture, mismatch = false): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const req = https.get(
      (mismatch ? f.mismatch_origin : f.origin) + "/readyz",
      {
        ca: mismatch ? f.mismatch_certificate : f.certificate,
        ...(mismatch ? { servername: "playwright-mismatch.invalid" } : {}),
        rejectUnauthorized: true,
        timeout: 15000,
      },
      (res) => {
        res.resume();
        if (
          res.statusCode !== 200 ||
          res.headers["x-gm-playwright-run"] !== f.nonce
        )
          reject(Error("PW_SERVER_IDENTITY"));
        else res.on("end", resolve);
      },
    );
    req.on("timeout", () => req.destroy(Error("PW_SERVER_TIMEOUT")));
    req.on("error", () => reject(Error("PW_SERVER_TLS")));
  });
}
