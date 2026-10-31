import { useState } from "react";
import { tr } from "./i18n";
import { Modal } from "./Modal";
import { clone, type Calendar, type Project } from "./types";
import {
  MIN_PLANNING_DATE,
  MAX_PLANNING_DATE,
  changeCalendarAnchor,
} from "./planningCalendar";
import {
  ProjectCalendars,
  ProvisionalCalendarNotice,
} from "./ProjectCalendars";
export function ProjectEditor({
  error,
  project,
  calendars,
  onSave,
  onDelete,
  onClose,
}: {
  error?: string;
  project: Project;
  calendars: Calendar[];
  onSave: (p: Project) => void;
  onDelete: () => Promise<void>;
  onClose: () => void;
}) {
  const [p, set] = useState(clone(project));
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deletePhrase, setDeletePhrase] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  async function removeProject() {
    setDeleting(true);
    setDeleteError("");
    try {
      await onDelete();
    } catch (e) {
      setDeleteError((e as Error).message || String(e));
    } finally {
      setDeleting(false);
    }
  }
  return (
    <Modal title={tr("md367c360b8")} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSave(p);
        }}
      >
        <label>
          {tr("m0918b4ba92")}
          <input
            value={p.title}
            onChange={(e) => set({ ...p, title: e.target.value })}
            maxLength={200}
            required
          />
        </label>
        <label>
          {tr("m4be3fc8ac8")}
          <textarea
            value={p.brief}
            onChange={(e) => set({ ...p, brief: e.target.value })}
          />
        </label>
        <div className="form-grid">
          <label>
            {tr("m200ee0c114")}
            <input
              type="date"
              min={MIN_PLANNING_DATE}
              max={MAX_PLANNING_DATE}
              value={p.start}
              onChange={(e) => set({ ...p, start: e.target.value })}
            />
          </label>
          <label>
            {tr("m2a32607189")}
            <select
              value={p.duration_mode}
              onChange={(e) =>
                set({
                  ...p,
                  duration_mode: e.target.value as Project["duration_mode"],
                })
              }
            >
              <option value="working">{tr("maa9d128bfa")}</option>
              <option value="calendar">{tr("m75e243c0d1")}</option>
            </select>
          </label>
        </div>
        <label>
          {tr("m222606ab1c")}
          <select
            data-calendar-anchor
            value={p.calendar_id}
            onChange={(e) =>
              set(changeCalendarAnchor(p, e.target.value, calendars))
            }
          >
            {calendars.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
        <ProjectCalendars project={p} calendars={calendars} onChange={set} />
        <ProvisionalCalendarNotice project={p} />
        <label>
          {tr("m43546d008b")}
          <input
            placeholder="PROJECT-123"
            value={p.external_reference}
            onChange={(e) => set({ ...p, external_reference: e.target.value })}
          />
        </label>
        <p className="notice">{tr("mb070fa572c")}</p>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <footer>
          <button>{tr("me9be6ac748")}</button>
        </footer>
      </form>
      <section className="danger-zone" aria-labelledby="delete-project-title">
        <h3 id="delete-project-title">{tr("mb8d719c6e9")}</h3>
        <p>{tr("me9d0d79799")}</p>
        {!confirmDelete ? (
          <button
            type="button"
            className="danger-button"
            onClick={() => {
              setConfirmDelete(true);
              setDeletePhrase("");
              setDeleteError("");
            }}
          >
            {tr("m054bd89afc")}
          </button>
        ) : (
          <>
            <p>
              {tr("m7b0ef4a06f")} <strong>{tr("m2977dad185")}</strong>.
            </p>
            <label>
              {tr("m8fe12b16bf")}
              <input
                autoFocus
                value={deletePhrase}
                onChange={(e) => setDeletePhrase(e.target.value)}
                autoComplete="off"
              />
            </label>
            {deleteError && (
              <p className="error" role="alert">
                {deleteError}
              </p>
            )}
            <div className="toolbar">
              <button
                type="button"
                className="secondary"
                disabled={deleting}
                onClick={() => {
                  setConfirmDelete(false);
                  setDeletePhrase("");
                  setDeleteError("");
                }}
              >
                {tr("m8fbe9b75cb")}
              </button>
              <button
                type="button"
                className="danger-button"
                disabled={deletePhrase.trim() !== tr("m2977dad185") || deleting}
                onClick={removeProject}
              >
                {deleting ? tr("m1180ece7ec") : tr("mbc2218eac3")}
              </button>
            </div>
          </>
        )}
      </section>
    </Modal>
  );
}
