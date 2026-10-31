import { useState } from "react";
import { tr } from "./i18n";
import { Modal } from "./Modal";
import { usePlanningClock } from "./PlanningClock";
import {
  clone,
  hours,
  type Participation,
  type Employee,
  type Activity,
} from "./types";
import {
  weekdays,
  weekdayNames,
  emptyActivity,
  hasParticipation,
  activityCapacity,
} from "./participation";
export function ActivityEditor({
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
