import { usePlanningClock } from "./PlanningClock";
import { tr } from "./i18n";
import { useSessionScope } from "./useSessionScope";
import { TaskEffort } from "./TaskEffort";
import { initials } from "./initials";
import {
  MAX_DURATION_DAYS,
  MIN_PLANNING_DATE,
  MAX_PLANNING_DATE,
  changeCalendarAnchor,
} from "./planningCalendar";
import {
  ProjectCalendars,
  ProvisionalCalendarNotice,
} from "./ProjectCalendars";
import { TaskComments } from "./TaskComments";
import { useEffect, useRef, useState } from "react";
import {
  blankTask,
  clone,
  hours,
  statuses,
  today,
  type Calendar,
  type Employee,
  type Link,
  type Project,
  type Task,
  type Participation,
  type Activity,
  type User,
} from "./types";
export function Modal({
  title,
  children,
  onClose,
  dismissible = true,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
  dismissible?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
    return () => ref.current?.close();
  }, []);
  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault();
        if (dismissible) onClose();
      }}
      onClick={(e) => {
        if (dismissible && e.target === ref.current) onClose();
      }}
    >
      <header>
        <h2>{title}</h2>
        <button
          type="button"
          className="icon"
          aria-label={tr("ma7a4033657")}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      {children}
    </dialog>
  );
}
export function TaskEditor({
  error,
  task,
  project,
  employees,
  onSave,
  onDelete,
  onChild,
  onClose,
  canManage,
  canWrite,
  user,
  onRefresh,
}: {
  error?: string;
  task: Task;
  project: Project;
  employees: Employee[];
  onSave: (t: Task) => void;
  onDelete: () => void;
  onChild: () => void;
  onClose: () => void;
  canManage: boolean;
  canWrite: boolean;
  user: User;
  onRefresh: () => Promise<void>;
}) {
  const [t, set] = useState(clone(task));
  const [effortBusy, setEffortBusy] = useState(false);
  const change = (k: keyof Task, v: any) => set({ ...t, [k]: v });
  const participants = employees.filter((e) =>
    project.people.some((p) => p.employee_id === e.id),
  );
  return (
    <Modal
      title={t.summary ? tr("m0dd94217b2") : tr("mb9ce27191d")}
      onClose={() => {
        if (!effortBusy) onClose();
      }}
      dismissible={false}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (canWrite && !effortBusy) onSave(t);
        }}
      >
        <label>
          {tr("m0918b4ba92")}
          <input
            value={t.title}
            onChange={(e) => change("title", e.target.value)}
            required
            maxLength={500}
            disabled={!canManage}
          />
        </label>
        <div className="form-grid">
          <label>
            {tr("m225077c6d4")}
            <select
              value={t.status}
              onChange={(e) => change("status", e.target.value)}
              disabled={t.summary || !canWrite}
            >
              {Object.entries(statuses).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          <label>
            {tr("mb0cedecc71")}
            <select
              value={t.parent}
              onChange={(e) => change("parent", e.target.value)}
              disabled={!canManage}
            >
              <option value="">{tr("m5ec6ff1590")}</option>
              {project.tasks
                .filter((x) => x.id !== t.id)
                .map((x) => (
                  <option key={x.id} value={x.id}>
                    {x.title}
                  </option>
                ))}
            </select>
          </label>
        </div>
        <label>
          {tr("m4be3fc8ac8")}
          <textarea
            value={t.brief}
            onChange={(e) => change("brief", e.target.value)}
            maxLength={4000}
            disabled={!canWrite}
          />
        </label>
        <label>
          {tr("mb3680f2cba")}
          <textarea
            value={t.description}
            onChange={(e) => change("description", e.target.value)}
            maxLength={20000}
            disabled={!canManage}
          />
        </label>
        <label>
          {tr("m2c2f44a8b4")}
          <textarea
            value={t.result}
            onChange={(e) => change("result", e.target.value)}
            disabled={!canManage}
          />
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={t.responsibility === "customer"}
            onChange={(e) =>
              change(
                "responsibility",
                e.target.checked ? "customer" : "internal",
              )
            }
            disabled={!canManage}
          />
          {tr("mff037c0ed0")}
        </label>
        {t.responsibility === "customer" && (
          <div className="customer-fields form-grid">
            <label>
              {tr("m837d5c467f")}
              <input
                value={t.customer_owner}
                onChange={(e) => change("customer_owner", e.target.value)}
                disabled={!canManage}
              />
            </label>
            <label>
              {tr("m3864b87a2c")}
              <input
                type="date"
                value={t.customer_due}
                onChange={(e) => change("customer_due", e.target.value)}
                disabled={!canManage}
              />
            </label>
            <label>
              {tr("m26fa892438")}
              <input
                type="email"
                aria-label={tr("m26fa892438")}
                value={t.customer_email || ""}
                onChange={(e) => change("customer_email", e.target.value)}
                maxLength={254}
                autoComplete="email"
                placeholder="name@example.com"
                disabled={!canManage}
              />
            </label>
          </div>
        )}
        <div className="form-grid">
          <label>
            {tr("maabf5bd7e2")}
            <input
              type="date"
              value={t.requested_start || t.start || project.start}
              onChange={(e) => change("requested_start", e.target.value)}
              disabled={!canManage || t.summary}
            />
          </label>
          <label>
            {tr("m455768fd15")}
            <input
              type="number"
              min={0}
              step={0.25}
              value={hours(t.planned_minutes)}
              max={1000000}
              onChange={(e) =>
                change(
                  "planned_minutes",
                  Math.round(Number(e.target.value) * 60),
                )
              }
              disabled={!canManage || t.summary}
            />
          </label>
        </div>
        {t.responsibility === "customer" && (
          <div className="form-grid">
            <label>
              {tr("m7e1eab95c5")}
              <input
                type="date"
                value={t.start}
                onChange={(e) => change("start", e.target.value)}
                disabled={!canManage}
              />
            </label>
            <label>
              {tr("md6a2c73a02")}
              <input
                type="date"
                value={t.end}
                onChange={(e) => change("end", e.target.value)}
                disabled={!canManage}
              />
            </label>
          </div>
        )}
        <label>
          {tr("m61738ad139")}
          <input
            type="number"
            min={0}
            max={MAX_DURATION_DAYS}
            data-duration-days
            step={1}
            value={t.duration_days || 0}
            onChange={(e) => change("duration_days", Number(e.target.value))}
            disabled={!canManage || t.summary || t.status === "done"}
          />
        </label>
        <p className="muted">{tr("m823c7a555d")}</p>
        <p className="muted">
          {tr("mc6d9143c13")} {t.start || tr("mb3067d0b57")} —{" "}
          {t.end || tr("ma8efd0c322")} {tr("m2685312b28")}{" "}
          {hours(
            project.tasks.find((current) => current.id === t.id)
              ?.actual_minutes || 0,
          )}{" "}
          {tr("m40831c5d98")}
        </p>
        {t.legacy_start && (
          <p className="muted">
            {tr("m9019b929fa")} {t.legacy_start} — {t.legacy_end}
            {tr("mb5d4fd0b26")}
          </p>
        )}
        {!t.summary && (
          <fieldset disabled={!canManage}>
            <legend>{tr("ma948189af7")}</legend>
            {participants.map((emp) => {
              const a = t.assignments.find((a) => a.employee_id === emp.id);
              const participation = project.people.find(
                (p) => p.employee_id === emp.id,
              );
              const limit = participationLimit(participation, emp);
              return (
                <div
                  className="assignment-availability"
                  data-assignment={emp.id}
                  key={emp.id}
                >
                  <div className="assignment">
                    <label className="check">
                      <input
                        aria-label={tr("m23a0d6bee7") + emp.name}
                        type="checkbox"
                        checked={!!a}
                        disabled={!a && limit <= 0}
                        onChange={(e) =>
                          change(
                            "assignments",
                            e.target.checked
                              ? [
                                  ...t.assignments,
                                  { employee_id: emp.id, daily_minutes: limit },
                                ]
                              : t.assignments.filter(
                                  (a) => a.employee_id !== emp.id,
                                ),
                          )
                        }
                      />
                      {emp.name}
                    </label>
                    {a && (
                      <label>
                        {tr("m9d883f1837")}
                        <input
                          aria-label={tr("m1e556df8e8") + emp.name}
                          type="number"
                          min={1 / 60}
                          max={24}
                          step="any"
                          value={hours(a.daily_minutes)}
                          onChange={(e) =>
                            change(
                              "assignments",
                              t.assignments.map((x) =>
                                x.employee_id === emp.id
                                  ? {
                                      ...x,
                                      daily_minutes: Math.round(
                                        Number(e.target.value) * 60,
                                      ),
                                    }
                                  : x,
                              ),
                            )
                          }
                        />
                      </label>
                    )}
                  </div>
                  <p className="muted" data-assignment-limit={emp.id}>
                    {tr("m20b83523c9")} {hours(emp.daily_minutes)}{" "}
                    {tr("mcdacae1667")} {hours(limit)} {tr("me0942c3cb4")}
                  </p>
                  {participation && (
                    <details className="assignment-schedule">
                      <summary>{tr("mc45b4c8963")}</summary>
                      <ParticipationDetails value={participation} />
                    </details>
                  )}
                  {limit <= 0 && (
                    <p className="notice amber">{tr("m91d9c06924")}</p>
                  )}
                </div>
              );
            })}
            {!participants.length && <p>{tr("m5e2bc2bd59")}</p>}
          </fieldset>
        )}
        <>
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
        </>
        <footer>
          {canManage && (
            <>
              <button
                type="button"
                className="danger secondary"
                disabled={effortBusy}
                onClick={onDelete}
              >
                {tr("mbe99b13612")}
              </button>
              <button
                type="button"
                className="secondary"
                disabled={
                  effortBusy || !project.tasks.some((x) => x.id === t.id)
                }
                title={tr("md3f9347e81")}
                onClick={onChild}
              >
                {tr("mb48ca44265")}
              </button>
            </>
          )}
          <button type="submit" disabled={!canWrite || effortBusy}>
            {tr("md5c911ec8d")}
          </button>
        </footer>
      </form>
      {project.tasks.some((item) => item.id === task.id) && (
        <TaskEffort
          task={project.tasks.find((item) => item.id === task.id)!}
          project={project}
          employees={employees}
          user={user}
          canWrite={canWrite}
          canManage={canManage}
          onRefresh={onRefresh}
          onBusy={setEffortBusy}
        />
      )}{" "}
      {project.tasks.some((item) => item.id === task.id) ? (
        <TaskComments
          key={project.id + "/" + task.id}
          projectId={project.id}
          taskId={task.id}
          recipientChanged={
            t.responsibility !== task.responsibility ||
            (t.customer_email || "") !== (task.customer_email || "")
          }
        />
      ) : (
        <p className="muted">{tr("m8c7c1741aa")}</p>
      )}
    </Modal>
  );
}
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
const weekdays = [1, 2, 3, 4, 5, 6, 0];
const weekdayNames = () => [
  tr("m7971972c7c"),
  tr("m31a8ee2fb6"),
  tr("m839b2d5c5a"),
  tr("m7ec16b9e41"),
  tr("m956618723d"),
  tr("m4ca114c23b"),
  tr("m95dab017a4"),
];
const emptyActivity = (): Activity => ({
  active: false,
  start: "",
  end: "",
  daily_minutes: 0,
});
const hasParticipation = (v: Participation) =>
  Object.keys(v.week || {}).length > 0;
function activityCapacity(v: Activity): number {
  const minutes = (s: string) => {
    const m = /^(\d{2}):(\d{2})$/.exec(s);
    return m ? Number(m[1]) * 60 + Number(m[2]) : NaN;
  };
  const span = minutes(v.end) - minutes(v.start);
  return v.active && span > 0 && Number(v.daily_minutes) > 0
    ? Math.min(span, Number(v.daily_minutes))
    : 0;
}
function participationLimit(
  v: Participation | undefined,
  employee: Employee,
): number {
  if (!v || !hasParticipation(v)) return 0;
  return Math.min(
    employee.daily_minutes,
    Math.max(
      0,
      ...Object.values(v.week || {}).map(activityCapacity),
      ...Object.values(v.exceptions || {}).map(activityCapacity),
    ),
  );
}
function ParticipationDetails({ value }: { value: Participation }) {
  return hasParticipation(value) ? (
    <ul className="participation-summary">
      {weekdays
        .filter((day) => value.week?.[String(day)]?.active)
        .map((day) => {
          const v = value.week![String(day)];
          return (
            <li key={day}>
              {weekdayNames()[day]} · {v.start}–{v.end} ·{" "}
              {hours(v.daily_minutes || 0)} {tr("m40831c5d98")}
            </li>
          );
        })}
      {!Object.values(value.week || {}).some((v) => v.active) && (
        <li>{tr("m28d4bea8af")}</li>
      )}
      {Object.keys(value.exceptions || {}).length > 0 && (
        <li>
          {tr("m44b1339295")} {Object.keys(value.exceptions).length}
        </li>
      )}
    </ul>
  ) : (
    <p className="notice amber" data-participation-missing={value.employee_id}>
      {tr("m00e3bd3f53")}{" "}
      {(value.weekdays || []).map((day) => weekdayNames()[day]).join(", ") ||
        tr("mf48f07aed8")}{" "}
      {value.window_start}–{value.window_end}.
    </p>
  );
}
export function PeoplePanel({
  project,
  employees,
  admin,
  canEdit,
  onChange,
  onRefresh,
  requestError,
}: {
  project: Project;
  employees: Employee[];
  admin: boolean;
  canEdit: boolean;
  onChange: (p: Project) => Promise<boolean>;
  onRefresh: () => Promise<void>;
  requestError?: string;
}) {
  const scope = useSessionScope();
  const api = scope.api;
  const [chosen, setChosen] = useState("");
  const [editing, setEditing] = useState<Participation | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  async function newEmployee() {
    try {
      await api("/employees", "POST", {
        id: "",
        name,
        daily_minutes: 480,
        user_id: "",
        provider: "",
        external_id: "",
      });
      setName("");
      await onRefresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }
  async function saveParticipation(v: Participation) {
    const p = clone(project);
    const index = p.people.findIndex((x) => x.employee_id === v.employee_id);
    if (index < 0) p.people.push(v);
    else p.people[index] = v;
    const ok = await onChange(p);
    if (ok) {
      setEditing(null);
      setChosen("");
    }
    return ok;
  }
  return (
    <section>
      <div className="section-title">
        <div>
          <h2>{tr("m8e2fbc67ad")}</h2>
          <p>{tr("m7bbfa77c19")}</p>
        </div>
      </div>
      {error && <p className="error">{error}</p>}
      <div className="toolbar">
        <select
          aria-label={tr("mf4191a9e8b")}
          value={chosen}
          disabled={!canEdit}
          onChange={(e) => setChosen(e.target.value)}
        >
          <option value="">{tr("m7cd59f175b")}</option>
          {employees
            .filter((e) => !project.people.some((p) => p.employee_id === e.id))
            .map((e) => (
              <option key={e.id} value={e.id}>
                {e.name}
              </option>
            ))}
        </select>
        <button
          disabled={!chosen || !canEdit}
          onClick={() =>
            setEditing({
              employee_id: chosen,
              weekdays: [],
              window_start: "",
              window_end: "",
              exceptions: {},
              week: {},
            })
          }
        >
          {tr("mb50e40a352")}
        </button>
      </div>
      <div className="people-grid">
        {project.people.map((v) => {
          const e = employees.find((e) => e.id === v.employee_id);
          return (
            <article
              className="person-card"
              key={v.employee_id}
              data-participant={v.employee_id}
            >
              <div className="avatar">{initials(e?.name)}</div>
              <h3>{e?.name || v.employee_id}</h3>
              <p>
                {hours(e?.daily_minutes || 0)} {tr("mffc07924d7")}
              </p>
              <ParticipationDetails value={v} />
              <button
                className="secondary"
                disabled={!canEdit}
                onClick={() => setEditing(clone(v))}
              >
                {hasParticipation(v) ? tr("mbf020f9374") : tr("m48bfe63f73")}
              </button>
            </article>
          );
        })}
      </div>
      {admin && canEdit && (
        <div className="panel">
          <h3>{tr("m98c0fefbe3")}</h3>
          <div className="toolbar">
            <input
              placeholder={tr("mbd391c57b0")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <button disabled={!name.trim()} onClick={newEmployee}>
              {tr("m161c628001")}
            </button>
          </div>
          <DirectorySearch onRefresh={onRefresh} />
        </div>
      )}
      {editing && (
        <ActivityEditor
          key={editing.employee_id}
          value={editing}
          employee={employees.find((e) => e.id === editing.employee_id)}
          isNew={
            !project.people.some((v) => v.employee_id === editing.employee_id)
          }
          requestError={requestError}
          onClose={() => setEditing(null)}
          onSave={saveParticipation}
        />
      )}
    </section>
  );
}
function ActivityEditor({
  value,
  employee,
  isNew,
  requestError,
  onSave,
  onClose,
}: {
  value: Participation;
  employee?: Employee;
  isNew: boolean;
  requestError?: string;
  onSave: (v: Participation) => Promise<boolean>;
  onClose: () => void;
}) {
  const [v, set] = useState<Participation>(() => {
    const next = clone(value);
    next.exceptions = next.exceptions || {};
    if (!hasParticipation(next))
      next.week = Object.fromEntries(
        weekdays.map((day) => [
          String(day),
          {
            active: (next.weekdays || []).includes(day),
            start: next.window_start || "",
            end: next.window_end || "",
            daily_minutes: 0,
          },
        ]),
      );
    return next;
  });
  const clock = usePlanningClock();
  const [d, setD] = useState(() => clock.today());
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const changeDay = (day: string, change: Partial<Activity>) =>
    set((current) => ({
      ...current,
      week: {
        ...current.week,
        [day]: { ...(current.week?.[day] || emptyActivity()), ...change },
      },
    }));
  const changeException = (day: string, change: Partial<Activity>) =>
    set((current) => ({
      ...current,
      exceptions: {
        ...current.exceptions,
        [day]: { ...current.exceptions[day], ...change },
      },
    }));
  function validate() {
    const entries = [
      ...Object.entries(v.week || {}).map(
        ([day, a]) => [weekdayNames()[Number(day)], a] as const,
      ),
      ...Object.entries(v.exceptions || {}),
    ];
    if (isNew && !entries.some(([, a]) => a.active)) return tr("m066ff0a965");
    for (const [label, a] of entries) {
      if (!a.active) continue;
      const cap = activityCapacity(a);
      if (!a.start || !a.end || a.end <= a.start)
        return label + tr("m5b6f80b127");
      if (
        !Number.isInteger(a.daily_minutes) ||
        !a.daily_minutes ||
        a.daily_minutes < 1 ||
        a.daily_minutes > 1440
      )
        return label + tr("m5cadadb3cb");
      if (cap < a.daily_minutes) return label + tr("mb414d6bf5b");
    }
    return "";
  }
  const monday = v.week?.["1"];
  return (
    <Modal
      title={isNew ? tr("mf28cb54fde") : tr("md3a5640b95")}
      onClose={onClose}
      dismissible={false}
    >
      <form
        className="participation-editor"
        onSubmit={async (e) => {
          e.preventDefault();
          const message = validate();
          setError(message);
          if (message) return;
          setSaving(true);
          try {
            if (!(await onSave(v))) setError(tr("m5848cc6a29"));
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setSaving(false);
          }
        }}
      >
        <p>
          <strong>{employee?.name || value.employee_id}</strong>{" "}
          {tr("m2efc63b3d6")} {hours(employee?.daily_minutes || 0)}{" "}
          {tr("md3ad3336d8")}
        </p>
        {!hasParticipation(value) && !isNew && (
          <p className="notice amber">{tr("m266c3fd3d6")}</p>
        )}
        <p className="muted">{tr("m9236fabf4b")}</p>
        <div className="participation-week">
          {weekdays.map((day) => {
            const a = v.week?.[String(day)] || emptyActivity();
            return (
              <div className="participation-day" data-weekday={day} key={day}>
                <label className="check">
                  <input
                    aria-label={weekdayNames()[day] + tr("m771dd5e27a")}
                    type="checkbox"
                    checked={a.active}
                    onChange={(e) =>
                      changeDay(String(day), { active: e.target.checked })
                    }
                  />
                  {weekdayNames()[day]}
                </label>
                <label>
                  {tr("m4af5d2efad")}
                  <input
                    aria-label={weekdayNames()[day] + tr("m10f6ea7bef")}
                    type="time"
                    disabled={!a.active}
                    required={a.active}
                    value={a.start}
                    onChange={(e) =>
                      changeDay(String(day), { start: e.target.value })
                    }
                  />
                </label>
                <label>
                  {tr("ma5787a1e68")}
                  <input
                    aria-label={weekdayNames()[day] + tr("mf3ea262536")}
                    type="time"
                    disabled={!a.active}
                    required={a.active}
                    value={a.end}
                    onChange={(e) =>
                      changeDay(String(day), { end: e.target.value })
                    }
                  />
                </label>
                <label>
                  {tr("m9d6c184850")}
                  <input
                    aria-label={weekdayNames()[day] + tr("mac8a5b002e")}
                    type="number"
                    min={1 / 60}
                    max={24}
                    step="any"
                    disabled={!a.active}
                    required={a.active}
                    placeholder={tr("mc7b490b23a")}
                    value={a.daily_minutes ? hours(a.daily_minutes) : ""}
                    onChange={(e) =>
                      changeDay(String(day), {
                        daily_minutes: Math.round(Number(e.target.value) * 60),
                      })
                    }
                  />
                </label>
              </div>
            );
          })}
        </div>
        <button
          type="button"
          className="secondary"
          disabled={
            !monday ||
            !monday.active ||
            !activityCapacity(monday) ||
            activityCapacity(monday) !== monday.daily_minutes
          }
          onClick={() =>
            set({
              ...v,
              week: {
                ...v.week,
                ...Object.fromEntries(
                  [1, 2, 3, 4, 5].map((day) => [String(day), { ...monday! }]),
                ),
              },
            })
          }
        >
          {tr("m00379686d6")}
        </button>
        <h3>{tr("m6fbbfcb3cb")}</h3>
        <div className="toolbar">
          <input
            aria-label={tr("m17af7907e9")}
            type="date"
            value={d}
            onChange={(e) => setD(e.target.value)}
          />
          <button
            type="button"
            className="secondary"
            disabled={!d}
            onClick={() =>
              set({
                ...v,
                exceptions: {
                  ...v.exceptions,
                  [d]: v.exceptions[d] || emptyActivity(),
                },
              })
            }
          >
            {tr("mc41bd2833e")}
          </button>
        </div>
        {Object.entries(v.exceptions)
          .sort()
          .map(([day, a]) => (
            <div
              className="participation-exception"
              data-exception={day}
              key={day}
            >
              <b>{day}</b>
              <label className="check">
                <input
                  aria-label={day + tr("m771dd5e27a")}
                  type="checkbox"
                  checked={a.active}
                  onChange={(e) =>
                    changeException(day, { active: e.target.checked })
                  }
                />
                {tr("ma87a4b39c3")}
              </label>
              {a.active && (
                <div className="participation-exception-inputs">
                  <label>
                    {tr("m4af5d2efad")}
                    <input
                      aria-label={day + tr("m10f6ea7bef")}
                      type="time"
                      required
                      value={a.start}
                      onChange={(e) =>
                        changeException(day, { start: e.target.value })
                      }
                    />
                  </label>
                  <label>
                    {tr("ma5787a1e68")}
                    <input
                      aria-label={day + tr("mf3ea262536")}
                      type="time"
                      required
                      value={a.end}
                      onChange={(e) =>
                        changeException(day, { end: e.target.value })
                      }
                    />
                  </label>
                  <label>
                    {tr("m9d6c184850")}
                    <input
                      aria-label={day + tr("mac8a5b002e")}
                      type="number"
                      min={1 / 60}
                      max={24}
                      step="any"
                      required
                      value={a.daily_minutes ? hours(a.daily_minutes) : ""}
                      onChange={(e) =>
                        changeException(day, {
                          daily_minutes: Math.round(
                            Number(e.target.value) * 60,
                          ),
                        })
                      }
                    />
                  </label>
                </div>
              )}
              <button
                type="button"
                className="icon"
                aria-label={tr("m81050e01d3") + day}
                onClick={() => {
                  const exceptions = { ...v.exceptions };
                  delete exceptions[day];
                  set({ ...v, exceptions });
                }}
              >
                ×
              </button>
            </div>
          ))}
        <p className="muted">{tr("m43f1b781ca")}</p>
        {(requestError || error) && (
          <p className="error" role="alert">
            {requestError || error}
          </p>
        )}
        <footer>
          <button disabled={saving}>
            {saving ? tr("m74d0346f0c") : tr("me9be6ac748")}
          </button>
        </footer>
      </form>
    </Modal>
  );
}
type DirectorySource = { provider: string; kind: "ldap" | "api" };
function DirectorySearch({ onRefresh }: { onRefresh: () => void }) {
  const scope = useSessionScope();
  const api = scope.api;
  const [q, setQ] = useState(""),
    [source, setSource] = useState<"ldap" | "api">("ldap"),
    [provider, setProvider] = useState("");
  const [sources, setSources] = useState<DirectorySource[]>([]),
    [rows, setRows] = useState<
      { id: string; name: string; email: string; provider: string }[]
    >([]),
    [msg, setMsg] = useState(""),
    [busy, setBusy] = useState(false);
  const version = useRef(0);
  useEffect(() => {
    let active = true;
    api<{ source_details: DirectorySource[] }>("/directory/status")
      .then((value) => {
        if (active) setSources(value.source_details || []);
      })
      .catch((e) => {
        if (active) setMsg(e.message);
      });
    return () => {
      active = false;
      version.current++;
    };
  }, []);
  function changeSource(next: "ldap" | "api") {
    version.current++;
    setSource(next);
    setProvider("");
    setRows([]);
    setMsg("");
    setBusy(false);
  }
  async function search() {
    const ticket = ++version.current;
    setBusy(true);
    try {
      const params = new URLSearchParams({
        q,
        source,
        ...(provider ? { provider } : {}),
      });
      const result = await api("/directory/people?" + params);
      if (ticket === version.current) {
        setRows(result);
        setMsg(result.length ? "" : tr("directoryEmpty"));
      }
    } catch (e) {
      if (ticket === version.current) setMsg((e as Error).message);
    } finally {
      if (ticket === version.current) setBusy(false);
    }
  }
  const choices = sources.filter((item) => item.kind === source);
  return (
    <>
      <h3>{tr("m0074c5a16c")}</h3>
      <div className="toolbar">
        <label>
          {tr("directorySource")}
          <select
            value={source}
            onChange={(e) => changeSource(e.target.value as "ldap" | "api")}
          >
            <option value="ldap">LDAP</option>
            <option value="api">{tr("directoryAPI")}</option>
          </select>
        </label>
        {choices.length > 0 && (
          <label>
            {tr("directoryProvider")}
            <select
              value={provider}
              onChange={(e) => {
                version.current++;
                setProvider(e.target.value);
                setRows([]);
                setMsg("");
                setBusy(false);
              }}
            >
              <option value="">{tr("directoryAllProviders")}</option>
              {choices.map((item) => (
                <option key={item.provider} value={item.provider}>
                  {item.provider}
                </option>
              ))}
            </select>
          </label>
        )}
        <input
          aria-label={tr("m20ee7e744c")}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={tr("m20ee7e744c")}
        />
        <button
          className="secondary"
          disabled={busy}
          onClick={() => void search()}
        >
          {busy ? tr("m74d0346f0c") : tr("mbe6156b8e4")}
        </button>
      </div>
      {msg && <p role="status">{msg}</p>}
      {rows.map((r) => (
        <div className="assignment" key={r.provider + "/" + r.id}>
          <span>
            {r.name}
            <small> · {r.provider}</small>
          </span>
          <button
            className="secondary"
            onClick={async () => {
              try {
                await api("/employees", "POST", {
                  id: "",
                  name: r.name,
                  daily_minutes: 480,
                  user_id: "",
                  provider: r.provider,
                  external_id: r.id,
                });
                setRows((old) =>
                  old.filter((x) => x.id !== r.id || x.provider !== r.provider),
                );
                onRefresh();
              } catch (e) {
                setMsg((e as Error).message);
              }
            }}
          >
            {tr("m71038c53bb")}
          </button>
        </div>
      ))}
    </>
  );
}
