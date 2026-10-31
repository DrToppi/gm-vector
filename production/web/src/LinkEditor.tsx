import { useState } from "react";
import { tr } from "./i18n";
import { Modal } from "./Modal";
import { clone, type Link, type Project } from "./types";
export function LinkEditor({
  error,
  link,
  project,
  onSave,
  onDelete,
  onClose,
}: {
  error?: string;
  link: Link;
  project: Project;
  onSave: (l: Link) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const [v, set] = useState(clone(link));
  return (
    <Modal title={tr("m44e0c835cf")} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSave(v);
        }}
      >
        {(["from", "to"] as const).map((k) => (
          <label key={k}>
            {k === "from" ? tr("meffbf5ac2f") : tr("m35fa56b8a9")}
            <select
              value={v[k]}
              onChange={(e) => set({ ...v, [k]: e.target.value })}
            >
              {project.tasks.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
        ))}
        <label>
          {tr("mfdf2e1d3dc")}
          <select
            value={v.mode}
            onChange={(e) =>
              set({ ...v, mode: e.target.value as Link["mode"], days: 0 })
            }
          >
            <option value="together">{tr("m561f7d8ef8")}</option>
            <option value="after">{tr("mf569188991")}</option>
            <option value="delay">{tr("m1c32d0cf6f")}</option>
          </select>
        </label>
        {v.mode === "delay" && (
          <label>
            {tr("mbdc32523b6")}
            <input
              type="number"
              min={0}
              max={3650}
              value={v.days}
              onChange={(e) => set({ ...v, days: Number(e.target.value) })}
            />
          </label>
        )}
        <p className="muted">{tr("ma34c7fcf63")}</p>
        <>
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
        </>
        <footer>
          <button type="button" className="danger secondary" onClick={onDelete}>
            {tr("m3e3f2f08de")}
          </button>
          <button>{tr("md5c911ec8d")}</button>
        </footer>
      </form>
    </Modal>
  );
}
