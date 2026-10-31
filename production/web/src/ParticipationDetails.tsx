import { tr } from "./i18n";
import { hours, type Participation } from "./types";
import { weekdays, weekdayNames, hasParticipation } from "./participation";
export function ParticipationDetails({ value }: { value: Participation }) {
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
