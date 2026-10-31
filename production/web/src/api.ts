import { projectWriteBody } from "./projectPayload";
import { tr, currentLocale } from "./i18n";
import {
  clearDiagnostics,
  diagnosticEpoch,
  recordRequest,
  setDiagnosticIdentity,
} from "./diagnostics";
export class ApiError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly status: number,
    public readonly requestId: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}
let csrf = "";
let generation = 0;
const requests = new Set<AbortController>();
const invalidation = new Set<() => void>();
export function setCSRF(s: string) {
  csrf = s;
}
export function sessionEpoch() {
  return generation;
}
export function isCurrentSession(epoch: number) {
  return epoch === generation;
}
export function assertSession(epoch: number) {
  if (!isCurrentSession(epoch))
    throw new DOMException(tr("m13ec26d9df"), "AbortError");
}
export function onSessionChange(callback: () => void) {
  invalidation.add(callback);
  return () => invalidation.delete(callback);
}
// Call at every boundary, including logout -> login of the same account.
export function advanceSession(keepCSRF = false) {
  generation++;
  if (!keepCSRF) csrf = "";
  for (const controller of requests) controller.abort();
  requests.clear();
  for (const callback of [...invalidation]) {
    try {
      callback();
    } catch {
      /* A closed print window must not prevent session invalidation. */
    }
  }
  invalidation.clear();
  setDiagnosticIdentity("");
  clearDiagnostics();
  return generation;
}
async function request<T = any>(
  epoch: number,
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  assertSession(epoch);
  const journalEpoch = diagnosticEpoch(),
    started = performance.now(),
    controller = new AbortController();
  let status = 0,
    request_id = "";
  const abort = () => controller.abort();
  if (signal?.aborted) abort();
  else signal?.addEventListener("abort", abort, { once: true });
  requests.add(controller);
  try {
    const res = await fetch("/api/v1" + path, {
      method,
      signal: controller.signal,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      },
      body:
        body === undefined
          ? undefined
          : JSON.stringify(projectWriteBody(path, body)),
    });
    assertSession(epoch);
    status = res.status;
    request_id = res.headers.get("X-Request-ID") || "";
    const data = await res.json();
    assertSession(epoch);
    if (!res.ok) {
      const code =
        typeof data.error?.code === "string" &&
        /^[a-zA-Z0-9_]{1,80}$/.test(data.error.code)
          ? data.error.code
          : "request_failed";
      const ref = /^[a-zA-Z0-9_-]{1,100}$/.test(request_id) ? request_id : "—";
      const detail =
        code === "calculation_budget_exceeded"
          ? tr("calendar.budgetExceeded")
          : currentLocale() === "ru" && typeof data.error?.message === "string"
            ? data.error.message
            : tr(
                code === "validation"
                  ? "apiValidation"
                  : status === 401
                    ? "apiUnauthorized"
                    : status === 403
                      ? "apiForbidden"
                      : status === 409
                        ? "apiConflict"
                        : status === 429
                          ? "apiRateLimit"
                          : "mee48dc403e",
              );
      throw new ApiError(
        detail + " " + tr("apiReference", { code, requestId: ref }),
        code,
        status,
        ref,
      );
    }
    return data;
  } catch (error) {
    assertSession(epoch);
    throw error;
  } finally {
    requests.delete(controller);
    signal?.removeEventListener("abort", abort);
    recordRequest(
      {
        path,
        method,
        status,
        request_id,
        duration_ms: performance.now() - started,
      },
      journalEpoch,
    );
  }
}
export function api<T = any>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  return request<T>(generation, path, method, body, signal);
}
export function sessionScope() {
  const epoch = generation;
  return {
    epoch,
    current: () => isCurrentSession(epoch),
    assert: () => assertSession(epoch),
    api: <T = any>(
      path: string,
      method = "GET",
      body?: unknown,
      signal?: AbortSignal,
    ) => request<T>(epoch, path, method, body, signal),
  };
}
export function download(
  name: string,
  body: string,
  type = "application/json",
) {
  const url = URL.createObjectURL(new Blob([body], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
