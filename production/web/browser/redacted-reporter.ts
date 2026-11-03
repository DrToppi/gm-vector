import fs from "node:fs";
import path from "node:path";
import type {
  Reporter,
  TestCase,
  TestResult,
  FullResult,
  TestError,
} from "@playwright/test/reporter";
import { loadFixture, scenarios, revisions } from "./contracts";
export default class RedactedReporter implements Reporter {
  private input = loadFixture();
  private results: { id: string; status: string; attempts: number }[] = [];
  private count = 0;
  private actualVersion = "";
  private nativeClipboard = "";
  private clipboardUI = "";
  private certificateRejections: string[] = [];
  private failurePhase = "";
  private failureSite = "";
  private failureClass = "";
  onStdErr(chunk: string | Buffer) {
    const value = String(chunk);
    if (
      process.env.GMA_PW_PRIVATE_DEBUG === "1" &&
      value.startsWith("PW_PRIVATE_ERROR ")
    )
      process.stderr.write(value.slice(0, 65536));
  }
  private debug(error?: TestError) {
    if (process.env.GMA_PW_PRIVATE_DEBUG !== "1" || !error) return;
    // The Go fixture directs both streams exclusively into its capped0600 log.
    // This explicit diagnostic mode never prints snippets/DOM/config/attachments.
    console.error(
      "PW_PRIVATE_ERROR " +
        JSON.stringify({
          message: error.message?.slice(0, 32768),
          stack: error.stack?.slice(0, 32768),
        }),
    );
  }
  onTestEnd(test: TestCase, result: TestResult) {
    this.count++;
    for (const item of test.annotations) {
      if (
        item.type === "pw-scenario" &&
        scenarios.includes(item.description as (typeof scenarios)[number])
      )
        this.results.push({
          id: item.description!,
          status: "passed",
          attempts: result.retry + 1,
        });
      if (
        item.type === "pw-browser-version" &&
        /^[0-9.]+$/.test(item.description || "")
      )
        this.actualVersion = item.description!;
      if (
        item.type === "pw-native-clipboard" &&
        ["write-succeeded", "write-denied", "api-unavailable"].includes(
          item.description || "",
        )
      )
        this.nativeClipboard = item.description!;
      if (
        item.type === "pw-clipboard-ui" &&
        item.description === "controlled-success-and-denial"
      )
        this.clipboardUI = item.description;
      if (
        item.type === "pw-certificate-rejection" &&
        item.description === this.input.fixture.engine + "-certificate-rejected"
      )
        this.certificateRejections.push(item.description);
    }
    if (result.status !== "passed") {
      for (const error of result.errors.slice(0, 3)) this.debug(error);
      this.failurePhase = scenarios[this.results.length] || "cleanup";
      this.failureClass =
        result.status === "timedOut" ? "TimeoutError" : "Error";
      this.failureSite =
        (result.errors[0]?.stack || "").match(
          /(?:critical\.spec|fixtures|contracts)\.ts:\d+:\d+/,
        )?.[0] || "";
    }
    // Do not print Playwright errors, call logs, snippets, attachments or DOM.
    if (result.status !== "passed")
      console.log(
        "PW_FAILURE engine=" +
          this.input.fixture.engine +
          " status=" +
          result.status +
          " exception=" +
          (result.status === "timedOut" ? "TimeoutError" : "Error"),
      );
  }
  onError(error?: TestError) {
    this.debug(error);
    console.log("PW_FAILURE kind=runner exception=Error");
  }
  onEnd(result: FullResult) {
    const f = this.input.fixture;
    const receipt = {
      format: "gm-vector-browser-receipt",
      schema_version: 1,
      run_nonce: f.nonce,
      engine: f.engine,
      playwright_version: "1.64.1",
      browser_revision: revisions[f.engine],
      browser_version: this.actualVersion,
      native_clipboard: this.nativeClipboard,
      clipboard_ui: this.clipboardUI,
      certificate_rejections: this.certificateRejections,
      status: result.status,
      test_count: this.count,
      ...(this.failurePhase
        ? {
            failure_phase: this.failurePhase,
            failure_site: this.failureSite,
            failure_class: this.failureClass,
          }
        : {}),
      scenarios: this.results,
      functional_tls: "owned-loopback-self-signed-exception",
      strict_tls:
        this.certificateRejections.length === 2
          ? "unknown-ca-and-mismatch-rejected-not-isolated-hostname-proof"
          : "not-completed",
    };
    const output = JSON.stringify(receipt);
    if (Buffer.byteLength(output) > 262144) throw Error("PW_RECEIPT_LIMIT");
    const file = path.join(this.input.directory, "receipt.json");
    fs.writeFileSync(file, output, { mode: 0o600, flag: "wx" });
    console.log(
      "PW_RESULT engine=" +
        f.engine +
        " status=" +
        result.status +
        " scenarios=" +
        this.results.length,
    );
  }
}
