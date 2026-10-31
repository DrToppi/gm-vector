import { useState } from "react";
import { tr } from "./i18n";
import { useSessionScope } from "./useSessionScope";
import {
  clone,
  hours,
  type Employee,
  type Project,
  type Participation,
} from "./types";
import { initials } from "./initials";
import { ActivityEditor } from "./ActivityEditor";
import { DirectorySearch } from "./DirectorySearch";
import { ParticipationDetails } from "./ParticipationDetails";
import { hasParticipation } from "./participation";
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
