import { useEffect, useRef, useState } from "react";
import { useSessionScope } from "./useSessionScope";
import { tr } from "./i18n";
import {
  detectProjectDocument,
  projectImportRequest,
  type TransferDocument,
  type ImportPreview,
} from "./projectTransfer";
export function ProjectImport({
  onCreated,
}: {
  onCreated: (id: string) => void | Promise<void>;
}) {
  const scope = useSessionScope(),
    generation = useRef(0),
    mounted = useRef(false);
  const [file, setFile] = useState<TransferDocument | null>(null),
    [preview, setPreview] = useState<ImportPreview | null>(null),
    [acknowledged, setAcknowledged] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      generation.current++;
    };
  }, []);
  const current = (version: number) =>
    mounted.current && scope.current() && version === generation.current;
  async function inspect(value: TransferDocument, version: number) {
    const request = projectImportRequest(value, true);
    const result = await scope.api<ImportPreview>(
      request.path,
      "POST",
      request.body,
    );
    if (current(version)) {
      setFile(value);
      setPreview(result);
      setAcknowledged(false);
    }
  }
  async function load(selected: File) {
    const version = ++generation.current;
    setBusy(true);
    setError("");
    setFile(null);
    setPreview(null);
    setAcknowledged(false);
    try {
      if (selected.size > 5 * 1024 * 1024) throw Error(tr("ma5aea96994"));
      const value = detectProjectDocument(JSON.parse(await selected.text()));
      if (current(version)) await inspect(value, version);
    } catch (cause) {
      if (current(version))
        setError(
          (cause as Error).message === "portable.invalidFile"
            ? tr("portable.invalidFile")
            : (cause as Error).message,
        );
    } finally {
      if (current(version)) setBusy(false);
    }
  }
  async function retry() {
    if (!file || busy) return;
    const version = ++generation.current;
    setBusy(true);
    setError("");
    setAcknowledged(false);
    try {
      await inspect(file, version);
    } catch (cause) {
      if (current(version)) setError((cause as Error).message);
    } finally {
      if (current(version)) setBusy(false);
    }
  }
  async function apply() {
    if (
      !file ||
      !preview ||
      busy ||
      (preview.timezone_acknowledgement_required && !acknowledged)
    )
      return;
    const version = ++generation.current;
    setBusy(true);
    setError("");
    const ack = preview.timezone_acknowledgement_required
      ? {
          source: preview.source_planning_timezone!,
          target: preview.target_planning_timezone!,
        }
      : undefined;
    try {
      const request = projectImportRequest(file, false, ack),
        project = await scope.api<{ id: string }>(
          request.path,
          "POST",
          request.body,
        );
      if (current(version)) await onCreated(project.id);
    } catch (cause) {
      if (current(version)) {
        setError((cause as Error).message);
        setAcknowledged(false);
      }
    } finally {
      if (current(version)) setBusy(false);
    }
  }
  return (
    <section className="new-project-import">
      <h3>{tr("m2d17c39362")}</h3>
      <label>
        {tr("mad0dc4044c")}
        <input
          type="file"
          accept=".json"
          disabled={busy}
          onChange={(event) => {
            const selected = event.target.files?.[0];
            event.target.value = "";
            if (selected) void load(selected);
          }}
        />
      </label>
      {busy && <p role="status">{tr("m297f881074")}</p>}
      {preview && file && (
        <>
          <p className="notice">{preview.message}</p>
          {file.kind === "legacy" ? (
            <p className="notice amber" data-import-provenance="unknown">
              {tr("portable.legacyUnknown")}
            </p>
          ) : (
            <p data-import-provenance="declared">
              {tr("portable.declaredZone", {
                zone:
                  preview.source_planning_timezone ||
                  file.document.metadata.planning_timezone,
              })}
            </p>
          )}
          {preview.timezone_acknowledgement_required && (
            <label className="check">
              <input
                type="checkbox"
                data-timezone-ack
                checked={acknowledged}
                onChange={(event) => setAcknowledged(event.target.checked)}
              />
              {tr("portable.acknowledge", {
                source: preview.source_planning_timezone,
                target: preview.target_planning_timezone,
              })}
            </label>
          )}
          <div className="toolbar">
            <button
              type="button"
              disabled={
                busy ||
                (!!preview.timezone_acknowledgement_required && !acknowledged)
              }
              onClick={() => void apply()}
            >
              {tr("mbce76d23c2")}
            </button>
            <button
              type="button"
              className="secondary"
              disabled={busy}
              onClick={() => void retry()}
            >
              {tr("portable.previewAgain")}
            </button>
          </div>
        </>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
