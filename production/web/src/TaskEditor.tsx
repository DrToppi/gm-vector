import { useState } from "react";
import { tr } from "./i18n";
import { Modal } from "./Modal";
import { TaskEffort } from "./TaskEffort";
import { TaskComments } from "./TaskComments";
import { participationLimit } from "./participation";
import { ParticipationDetails } from "./ParticipationDetails";
import { MAX_DURATION_DAYS } from "./planningCalendar";
import {
  clone,
  hours,
  statuses,
  type Task,
  type Project,
  type Employee,
  type User,
} from "./types";
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
