import { useEffect, useRef, useState } from "react";
import { ApiError, download, onSessionChange } from "./api";
import { useSessionScope } from "./useSessionScope";
import { tr } from "./i18n";
import {
  historyBase,
  type HistoryFilters,
  type HistoryExport,
} from "./history";
import { historyReport, HistoryConversionLimit } from "./historyReport";

export function historyError(error: unknown): string {
  const keys: Record<string, string> = {
    history_changed: "history.changed",
    invalid_cursor: "history.invalidCursor",
    cursor_expired: "history.expired",
    history_export_busy: "history.exportBusy",
    history_export_limit: "history.exportLimit",
    history_export_timeout: "history.exportTimeout",
  };
  if (error instanceof HistoryConversionLimit)
    return tr("history.conversionLimit");
  if (error instanceof ApiError && keys[error.code])
    return (
      tr(keys[error.code]) +
      " " +
      tr("apiReference", { code: error.code, requestId: error.requestId })
    );
  return error instanceof Error ? error.message : tr("history.loadError");
}
export function HistoryExportActions({
  projectID,
  filters,
  revision,
}: {
  projectID: string;
  filters: HistoryFilters;
  revision: number;
}) {
  const scope = useSessionScope(),
    signature = projectID + "/" + revision + "/" + JSON.stringify(filters),
    context = useRef(signature);
  context.current = signature;
  const generation = useRef(0),
    active = useRef(false),
    controller = useRef<AbortController | null>(null),
    prints = useRef(new Set<Window>());
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const closePrint = (owned: Window | null) => {
    if (owned) {
      owned.close();
      prints.current.delete(owned);
    }
  };
  useEffect(() => {
    active.current = true;
    generation.current++;
    setBusy(false);
    setError("");
    const closeAll = () => {
      for (const owned of prints.current) owned.close();
      prints.current.clear();
    };
    // A print dialog returning does not close its window. Retain every owned
    // window until the user closes it, the context changes or the session ends.
    const stop = onSessionChange(closeAll),
      sweep = window.setInterval(() => {
        for (const owned of prints.current)
          if (owned.closed) prints.current.delete(owned);
      }, 1000);
    return () => {
      active.current = false;
      generation.current++;
      controller.current?.abort();
      window.clearInterval(sweep);
      stop();
      closeAll();
    };
  }, [signature]);
  async function exportHistory(format: "json" | "csv" | "html" | "pdf") {
    if (busy || !active.current) return;
    const version = ++generation.current,
      key = signature,
      windowForPrint = format === "pdf" ? window.open("", "_blank") : null;
    if (format === "pdf" && !windowForPrint) {
      setError(tr("m2891165d5a"));
      return;
    }
    if (windowForPrint) prints.current.add(windowForPrint);
    controller.current = new AbortController();
    setBusy(true);
    setError("");
    const current = () =>
      active.current &&
      scope.current() &&
      context.current === key &&
      generation.current === version;
    try {
      const document = await scope.api<HistoryExport>(
        historyBase(projectID) + "/export?" + new URLSearchParams(filters),
        "GET",
        undefined,
        controller.current.signal,
      );
      if (!current()) {
        closePrint(windowForPrint);
        return;
      }
      const text = historyReport(document, format);
      if (!current()) {
        closePrint(windowForPrint);
        return;
      }
      if (windowForPrint) {
        windowForPrint.opener = null;
        windowForPrint.document.open();
        windowForPrint.document.write(text);
        windowForPrint.document.close();
        windowForPrint.focus();
        window.setTimeout(() => {
          if (current() && !windowForPrint.closed) windowForPrint.print();
          else closePrint(windowForPrint);
        }, 150);
      } else
        download(
          "gm-vector-time-history." + format,
          text,
          format === "json"
            ? "application/json"
            : format === "csv"
              ? "text/csv;charset=utf-8"
              : "text/html;charset=utf-8",
        );
    } catch (cause) {
      closePrint(windowForPrint);
      if (current() && (cause as Error).name !== "AbortError")
        setError(historyError(cause));
    } finally {
      if (current()) setBusy(false);
    }
  }
  return (
    <div className="history-export">
      <details>
        <summary>{tr("history.export")}</summary>
        <div className="toolbar">
          {(["json", "csv", "html", "pdf"] as const).map((format) => (
            <button
              type="button"
              className="secondary"
              key={format}
              disabled={busy}
              data-history-export={format}
              onClick={() => void exportHistory(format)}
            >
              {format.toUpperCase()}
            </button>
          ))}
        </div>
      </details>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
