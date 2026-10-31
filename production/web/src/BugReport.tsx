import { tr } from "./i18n";
import { githubIssueDraft, issueBaseURL } from "./githubIssue";
import { useSessionScope } from "./useSessionScope";
import { useCallback, useEffect, useRef, useState } from "react";
import { download, ApiError } from "./api";
import { Modal } from "./Editors";
import {
  diagnosticEnvironment,
  diagnosticSnapshot,
  safeServerEvents,
  safeServerState,
  safeView,
} from "./diagnostics";
import {
  encryptReport,
  validateReportPublicKey,
  type ReportPublicKey,
} from "./reportCrypto";

type Fields = {
  title: string;
  steps: string;
  expected: string;
  actual: string;
};
const empty: Fields = { title: "", steps: "", expected: "", actual: "" };
const labels: Record<keyof Fields, string> = {
  get title() {
    return tr("m78eedb35fc");
  },
  get steps() {
    return tr("ma47b55bd9c");
  },
  get expected() {
    return tr("m2c2f44a8b4");
  },
  get actual() {
    return tr("m1a02d63d94");
  },
};
// Only fixed public API messages may be shown; never echo arbitrary response text.
const uploadErrorKeys: Record<string, string> = {
  report_storage_unavailable: "m63248b08fb",
  report_storage_busy: "m1251086afd",
  report_storage_full: "mf7fd5a5984",
  report_rate_limit: "m0597acc834",
  report_too_large: "m75557dbdb6",
  validation: "mc09bfd17ee",
};
// Bounded wait keeps the local report usable when the API is unresponsive.
async function limited<T>(
  operation: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  try {
    return await operation(controller.signal);
  } finally {
    clearTimeout(timer);
  }
}
export function BugReport({
  authenticated = false,
  admin = false,
  view = "login",
}: {
  authenticated?: boolean;
  admin?: boolean;
  view?: string;
}) {
  const scope = useSessionScope();
  const api = scope.api;
  const [open, setOpen] = useState(false),
    [fields, setFields] = useState<Fields>(empty),
    [report, setReport] = useState<object | null>(null),
    [reviewed, setReviewed] = useState(false),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState(""),
    [key, setKey] = useState<ReportPublicKey | null>(null),
    [keyState, setKeyState] = useState<
      "loading" | "ready" | "cached" | "missing" | "unavailable"
    >("loading");
  const cachedKey = useRef<ReportPublicKey | null>(null),
    keyRequest = useRef(0),
    pendingUpload = useRef<{
      report: object;
      kid: string;
      encrypted: string;
    } | null>(null);
  const [savedID, setSavedID] = useState("");
  const [issue, setIssue] = useState<ReturnType<
      typeof githubIssueDraft
    > | null>(null),
    [publicReviewed, setPublicReviewed] = useState(false),
    [issueMessage, setIssueMessage] = useState("");
  const loadKey = useCallback(async () => {
    const request = ++keyRequest.current;
    setKeyState("loading");
    let responseReceived = false;
    try {
      const result = await limited((signal) =>
        api<{ configured: boolean; key?: ReportPublicKey }>(
          "/diagnostics/key",
          "GET",
          undefined,
          signal,
        ),
      );
      responseReceived = true;
      if (request !== keyRequest.current) return;
      if (result.configured && result.key) {
        await validateReportPublicKey(result.key);
        if (request !== keyRequest.current) return;
        cachedKey.current = result.key;
        setKey(result.key);
        setKeyState("ready");
      } else {
        cachedKey.current = null;
        setKey(null);
        setKeyState("missing");
      }
    } catch {
      if (request !== keyRequest.current) return;
      if (responseReceived) {
        cachedKey.current = null;
        setKey(null);
        setKeyState("unavailable");
      } else if (cachedKey.current) {
        setKey(cachedKey.current);
        setKeyState("cached");
      } else {
        setKeyState("unavailable");
      }
    }
  }, [api]);
  useEffect(() => {
    void loadKey();
  }, [loadKey]);
  function change(field: keyof Fields, value: string) {
    setIssue(null);
    setPublicReviewed(false);
    setIssueMessage("");
    setFields({ ...fields, [field]: value });
    setReport(null);
    setReviewed(false);
    setMessage("");
    setSavedID("");
    pendingUpload.current = null;
  }
  async function prepare() {
    setBusy(true);
    setReviewed(false);
    setMessage("");
    setSavedID("");
    pendingUpload.current = null;
    const server: {
      events_status: string;
      events: ReturnType<typeof safeServerEvents>;
      state_status: string;
      state?: ReturnType<typeof safeServerState>;
    } = {
      events_status: authenticated ? "unavailable" : "not_authenticated",
      events: [],
      state_status: admin ? "unavailable" : "not_available_for_role",
    };
    if (authenticated) {
      await Promise.all([
        limited((signal) =>
          api("/diagnostics/events", "GET", undefined, signal),
        )
          .then((data) => {
            server.events = safeServerEvents(data);
            server.events_status = "available";
          })
          .catch(() => {}),
        admin
          ? limited((signal) => api("/diagnostics", "GET", undefined, signal))
              .then((data) => {
                server.state = safeServerState(data);
                server.state_status = "available";
              })
              .catch(() => {})
          : Promise.resolve(),
      ]);
    }
    const text = Object.entries(labels)
      .map(([k, label]) => label + "\n" + fields[k as keyof Fields])
      .join("\n\n");
    setReport({
      format: "gma-bug-report",
      schema_version: 1,
      created_at: new Date().toISOString(),
      report: fields,
      report_text: text,
      context: { view: safeView(view) },
      environment: diagnosticEnvironment(),
      client: {
        retention_minutes: 15,
        capacity: 100,
        events: diagnosticSnapshot(),
      },
      server,
    });
    setBusy(false);
  }
  async function save() {
    if (!key || !report || !reviewed || busy) return;
    setBusy(true);
    setMessage("");
    try {
      const encrypted = await encryptReport(report, key);
      scope.assert();
      download(
        "gma-bug-" +
          new Date().toISOString().replace(/[:.]/g, "-") +
          ".gma-bug",
        encrypted,
        "application/jose",
      );
      setMessage(tr("m1b5c511f44"));
    } catch {
      setMessage(tr("m4bdfc4b355"));
    } finally {
      setBusy(false);
    }
  }
  async function store() {
    if (!key || !report || !reviewed || busy || savedID) return;
    setBusy(true);
    setMessage("");
    let uploading = false;
    try {
      if (
        pendingUpload.current?.report !== report ||
        pendingUpload.current.kid !== key.kid
      ) {
        const encrypted = await encryptReport(report, key);
        scope.assert();
        pendingUpload.current = { report, kid: key.kid, encrypted };
      }
      scope.assert();
      uploading = true;
      const result = await limited((signal) =>
        api<{ id: string; stored: boolean }>(
          "/diagnostics/reports",
          "POST",
          { encrypted: pendingUpload.current!.encrypted },
          signal,
        ),
      );
      scope.assert();
      if (
        result.stored !== true ||
        typeof result.id !== "string" ||
        !/^[-_a-zA-Z0-9]{16,128}$/.test(result.id)
      )
        throw new Error("Invalid receipt");
      setSavedID(result.id);
      setMessage(tr("m2471610606") + result.id + ".");
    } catch (error) {
      if (scope.current())
        setMessage(
          uploading &&
            error instanceof ApiError &&
            Object.hasOwn(uploadErrorKeys, error.code)
            ? tr(uploadErrorKeys[error.code])
            : uploading
              ? tr("m20846080ba")
              : tr("m1b6001cc26"),
        );
    } finally {
      if (scope.current()) setBusy(false);
    }
  }
  return (
    <>
      <footer className="bug-report-launcher">
        <button
          className="secondary"
          data-bug-report="open"
          onClick={() => {
            setOpen(true);
            void loadKey();
          }}
        >
          Bug Report
        </button>
      </footer>
      {open && (
        <Modal
          title="Bug Report"
          dismissible={false}
          onClose={() => {
            if (!busy) setOpen(false);
          }}
        >
          <p>{tr("me48e227d16")}</p>
          <form
            data-bug-report="form"
            onSubmit={(e) => {
              e.preventDefault();
              void prepare();
            }}
          >
            {(Object.keys(labels) as (keyof Fields)[]).map((field) => (
              <label key={field}>
                {labels[field]}
                {field === "title" ? (
                  <input
                    data-bug-report-field={field}
                    value={fields[field]}
                    onChange={(e) => change(field, e.target.value)}
                    required
                    maxLength={200}
                    disabled={busy}
                  />
                ) : (
                  <textarea
                    data-bug-report-field={field}
                    value={fields[field]}
                    onChange={(e) => change(field, e.target.value)}
                    required
                    maxLength={4000}
                    disabled={busy}
                  />
                )}
              </label>
            ))}
            <p className="muted">
              {tr("m03885dca4a")}{" "}
              {admin ? tr("m92ea011fb8") : tr("m7dd4cb85f3")}{" "}
              {tr("m88bb5d28e8")}
            </p>
            <button
              type="submit"
              className="secondary"
              data-bug-report="prepare"
              disabled={
                busy ||
                !fields.title.trim() ||
                !fields.steps.trim() ||
                !fields.expected.trim() ||
                !fields.actual.trim()
              }
            >
              {busy ? tr("me261fc0388") : tr("m8a13a972c6")}
            </button>
          </form>
          {report && (
            <section className="bug-report-preview">
              <h3>{tr("md79c07611e")}</h3>
              <pre data-bug-report="preview" tabIndex={0}>
                {JSON.stringify(report, null, 2)}
              </pre>
              <label className="check">
                <input
                  type="checkbox"
                  data-bug-report="reviewed"
                  checked={reviewed}
                  disabled={busy}
                  onChange={(e) => setReviewed(e.target.checked)}
                />
                {tr("mb4fdd8cf88")}
              </label>
            </section>
          )}
          {keyState !== "ready" && (
            <p className="notice" data-bug-report="key-status">
              {keyState === "cached"
                ? tr("mbbabe5facf")
                : keyState === "loading"
                  ? tr("m78a1aa5799")
                  : keyState === "missing"
                    ? tr("m64006fb2b0")
                    : tr("m1d0833a607")}{" "}
              {keyState !== "loading" && (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void loadKey()}
                >
                  {tr("m5189135a61")}
                </button>
              )}
            </p>
          )}
          {message && (
            <p role="status" className="notice" data-bug-report="message">
              {message}
            </p>
          )}
          <footer>
            <button
              type="button"
              data-bug-report="store"
              disabled={
                !report ||
                !reviewed ||
                !key ||
                !["ready", "cached"].includes(keyState) ||
                busy ||
                !!savedID
              }
              onClick={() => void store()}
            >
              {savedID ? tr("m53e372b0ec") : tr("m190e3e09f5")}
            </button>
            <button
              type="button"
              className="secondary"
              data-bug-report="download"
              disabled={
                !report ||
                !reviewed ||
                !key ||
                !["ready", "cached"].includes(keyState) ||
                busy
              }
              onClick={() => void save()}
            >
              {tr("md8fdec9b2d")}
            </button>
          </footer>
          <section className="public-issue">
            <h3>{tr("issueHeading")}</h3>
            <p>{tr("issuePrivacy")}</p>
            <button
              type="button"
              className="secondary"
              data-bug-report="issue-preview"
              disabled={
                busy ||
                !fields.title.trim() ||
                !fields.steps.trim() ||
                !fields.expected.trim() ||
                !fields.actual.trim()
              }
              onClick={() => {
                setIssue(
                  githubIssueDraft(
                    fields,
                    (
                      report as {
                        server?: { state?: { version?: string } };
                      } | null
                    )?.server?.state?.version,
                  ),
                );
                setPublicReviewed(false);
                setIssueMessage("");
              }}
            >
              {tr("issuePreview")}
            </button>
            {issue && (
              <>
                <h4>{tr("issueExactPreview")}</h4>
                <strong data-bug-report="issue-title">{issue.title}</strong>
                <pre data-bug-report="issue-body" tabIndex={0}>
                  {issue.body}
                </pre>
                <label className="check">
                  <input
                    type="checkbox"
                    data-bug-report="issue-reviewed"
                    checked={publicReviewed}
                    onChange={(e) => setPublicReviewed(e.target.checked)}
                  />
                  {tr("issuePublicConsent")}
                </label>
                {!issue.fits && <p className="notice">{tr("issueTooLong")}</p>}
                <div className="toolbar">
                  <button
                    type="button"
                    className="secondary"
                    data-bug-report="issue-copy"
                    onClick={async () => {
                      try {
                        await navigator.clipboard.writeText(
                          issue.title + "\n\n" + issue.body,
                        );
                        setIssueMessage(tr("issueCopied"));
                      } catch {
                        setIssueMessage(tr("issueCopyError"));
                      }
                    }}
                  >
                    {tr("issueCopy")}
                  </button>
                  {publicReviewed ? (
                    <a
                      className="issue-link"
                      data-bug-report="issue-open"
                      href={issue.fits ? issue.url : issueBaseURL}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      {tr(issue.fits ? "issueOpen" : "issueOpenBlank")}
                    </a>
                  ) : (
                    <button type="button" disabled>
                      {tr(issue.fits ? "issueOpen" : "issueOpenBlank")}
                    </button>
                  )}
                </div>
                {issueMessage && <p role="status">{issueMessage}</p>}
              </>
            )}
          </section>
        </Modal>
      )}
    </>
  );
}
