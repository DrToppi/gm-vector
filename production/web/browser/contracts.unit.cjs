// Bounded, synthetic IPC/receipt/privacy controls. No browser, DB or network.
const fs = require("node:fs"),
  os = require("node:os"),
  path = require("node:path"),
  vm = require("node:vm"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto");
const ts = require("../node_modules/typescript");
const output = [],
  cache = new Map();
function load(name) {
  if (cache.has(name)) return cache.get(name);
  const exports = {},
    sandbox = {
      exports,
      Buffer,
      URL,
      process,
      console: { log: (value) => output.push(String(value)) },
      require: (specifier) =>
        specifier.startsWith("./")
          ? load(specifier.slice(2))
          : require(specifier),
    };
  const source = fs.readFileSync(path.join(__dirname, name + ".ts"), "utf8");
  vm.runInNewContext(
    ts.transpileModule(source, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        esModuleInterop: true,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    sandbox,
  );
  cache.set(name, exports);
  return exports;
}
const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gm-vector-pw-unit-"));
fs.chmodSync(directory, 0o700);
const previous = process.env.GMA_PW_FIXTURE_FILE;
try {
  const {
      loadFixture,
      validateFixture,
      validatePackages,
      validateHost,
      revisions,
      versions,
    } = load("contracts"),
    marker = "PW_PRIVATE_" + crypto.randomBytes(16).toString("hex");
  const fixture = {
    format: "gm-vector-playwright-fixture",
    schema_version: 1,
    engine: "chromium",
    nonce: crypto.randomBytes(32).toString("base64url"),
    origin: "https://127.0.0.1:43199",
    certificate: "-----BEGIN CERTIFICATE-----\n" + marker,
    username: "synthetic",
    password: marker,
    report_private_key: "-----BEGIN PRIVATE KEY-----\n" + marker,
    calendar_id: "synthetic",
    history_project: "synthetic",
  };
  const file = path.join(directory, "fixture.json");
  const write = (data) =>
    fs.writeFileSync(
      file,
      typeof data === "string" ? data : JSON.stringify(data),
      { mode: 0o600 },
    );
  process.env.GMA_PW_FIXTURE_FILE = file;
  write(fixture);
  for (const release of [
    'ID=ubuntu\nVERSION_ID="24.04"',
    'ID=ubuntu\nVERSION_ID="26.04"',
  ])
    validateHost("linux", "x64", release, "24.21.0");
  for (const args of [
    ["win32", "x64", "ID=ubuntu\nVERSION_ID=24.04", "24.21.0"],
    ["linux", "arm64", "ID=ubuntu\nVERSION_ID=24.04", "24.21.0"],
    ["linux", "x64", "ID=ubuntu\nVERSION_ID=22.04", "24.21.0"],
    ["linux", "x64", "ID=debian\nVERSION_ID=24.04", "24.21.0"],
    ["linux", "x64", "ID=ubuntu\nVERSION_ID=24.04", "22.0.0"],
  ])
    assert.throws(() => validateHost(...args));
  const packages = Array.from({ length: 3 }, () => ({ version: "1.64.1" }));
  const browsers = [
    "chromium",
    "firefox",
    "webkit",
    "chromium-headless-shell",
  ].map((name) => {
    const engine = name === "chromium-headless-shell" ? "chromium" : name;
    return {
      name,
      revision: revisions[engine],
      browserVersion: versions[engine],
    };
  });
  validatePackages(packages, { browsers });
  assert.throws(() =>
    validatePackages([{ version: "1.0.0" }, ...packages.slice(1)], {
      browsers,
    }),
  );
  assert.throws(() =>
    validatePackages(packages, { browsers: browsers.slice(1) }),
  );
  assert.throws(() =>
    validatePackages(packages, {
      browsers: browsers.map((item, i) =>
        i ? item : { ...item, revision: "old" },
      ),
    }),
  );
  assert.equal(loadFixture().fixture.engine, "chromium");
  for (const origin of [
    "http://127.0.0.1:43199",
    "https://example.com",
    "https://127.0.0.1:9443",
    "https://user@127.0.0.1:43199",
    "https://127.0.0.1:43199/path",
    "https://127.0.0.1:43199/#fragment",
  ])
    assert.throws(() => validateFixture({ ...fixture, origin }));
  for (const invalid of [
    { ...fixture, engine: "missing" },
    { ...fixture, nonce: "short" },
    { ...fixture, unexpected: 1 },
    { ...fixture, password: 123 },
  ])
    assert.throws(() => validateFixture(invalid));
  fs.chmodSync(file, 0o644);
  assert.throws(() => loadFixture());
  fs.chmodSync(file, 0o600);
  fs.chmodSync(directory, 0o755);
  assert.throws(() => loadFixture());
  fs.chmodSync(directory, 0o700);
  write(" ".repeat(65537));
  assert.throws(() => loadFixture());
  write(
    JSON.stringify(fixture).replace(
      '"schema_version":1',
      '"schema_version":2,"schema_version":1',
    ),
  );
  assert.throws(() => loadFixture());
  write(fixture);
  const link = path.join(directory, "link.json");
  fs.symlinkSync(file, link);
  process.env.GMA_PW_FIXTURE_FILE = link;
  assert.throws(() => loadFixture());
  const linkedParent = path.join(directory, "parent-link");
  fs.symlinkSync(directory, linkedParent);
  process.env.GMA_PW_FIXTURE_FILE = path.join(linkedParent, "fixture.json");
  assert.throws(() => loadFixture());
  process.env.GMA_PW_FIXTURE_FILE = file;
  const Reporter = load("redacted-reporter").default,
    reporter = new Reporter();
  reporter.onTestEnd(
    {
      annotations: [
        { type: "pw-scenario", description: marker },
        { type: "pw-browser-version", description: marker },
      ],
    },
    {
      status: "failed",
      retry: 0,
      errors: [
        { stack: marker + "\n at critical.spec.ts:12:3", message: marker },
      ],
    },
  );
  reporter.onError();
  reporter.onEnd({ status: "failed" });
  assert(!output.join("\n").includes(marker));
  const receipt = JSON.parse(
    fs.readFileSync(path.join(directory, "receipt.json"), "utf8"),
  );
  assert.equal(receipt.status, "failed");
  assert.equal(receipt.failure_site, "critical.spec.ts:12:3");
  assert.equal(receipt.scenarios.length, 0);
  assert.equal(receipt.browser_version, "");
  assert(!JSON.stringify(receipt).includes(marker));
  assert.throws(
    () => reporter.onEnd({ status: "failed" }),
    (error) => error.code === "EEXIST",
  );
  console.log(
    "Playwright contract unit PASS: origin/schema/ownership/mode/symlink/size/duplicate-key guards, private exclusive receipt, adversarial secret-free reporter",
  );
} catch {
  console.error(
    "Playwright contract unit FAIL: Error; no raw assertion/config output",
  );
  process.exitCode = 1;
} finally {
  if (previous === undefined) delete process.env.GMA_PW_FIXTURE_FILE;
  else process.env.GMA_PW_FIXTURE_FILE = previous;
  fs.rmSync(directory, { recursive: true, force: true });
}
