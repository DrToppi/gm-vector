import { tr } from "./i18n";
import { calendarPins, selectCalendarYear } from "./planningCalendar";
import type { Calendar, Project } from "./types";

export function ProvisionalCalendarNotice({ project }: { project: Project }) {
  const years = [
    ...new Set(
      (project.provisional_years || []).filter(
        (year) => Number.isInteger(year) && year >= 2000 && year <= 2200,
      ),
    ),
  ].sort((a, b) => a - b);
  return years.length ? (
    <div
      className="notice amber provisional-calendar"
      role="status"
      data-provisional-years={years.join(",")}
    >
      <strong>{tr("calendar.provisionalTitle")}</strong>
      <p>{tr("calendar.provisionalNotice", { years: years.join(", ") })}</p>
    </div>
  ) : null;
}

export function ProjectCalendars({
  project,
  calendars,
  onChange,
}: {
  project: Project;
  calendars: Calendar[];
  onChange: (value: Project) => void;
}) {
  const pins = calendarPins(project, calendars);
  const years = [
    ...new Set([
      ...calendars.map((calendar) => calendar.year),
      ...Object.keys(pins).map(Number),
    ]),
  ]
    .filter((year) => Number.isInteger(year) && year >= 2000 && year <= 2200)
    .sort((a, b) => a - b);
  const anchor = calendars.find(
    (calendar) => calendar.id === project.calendar_id,
  );
  return (
    <fieldset className="annual-calendars">
      <legend>{tr("calendar.annualTitle")}</legend>
      <p>{tr("calendar.annualHelp")}</p>
      {years.map((year) => {
        const options = calendars.filter((calendar) => calendar.year === year);
        const selected = pins[String(year)] || "";
        return (
          <label key={year}>
            {tr("calendar.year", { year })}
            <select
              data-calendar-year={year}
              value={selected}
              onChange={(event) =>
                onChange(
                  selectCalendarYear(
                    project,
                    String(year),
                    event.target.value,
                    calendars,
                  ),
                )
              }
            >
              <option value="" disabled={anchor?.year === year}>
                {tr("calendar.provisionalOption")}
              </option>
              {selected &&
                !options.some((calendar) => calendar.id === selected) && (
                  <option value={selected}>
                    {tr("calendar.unavailableVersion", { id: selected })}
                  </option>
                )}
              {options.map((calendar) => (
                <option key={calendar.id} value={calendar.id}>
                  {calendar.title}
                </option>
              ))}
            </select>
            {anchor?.year === year && (
              <small>{tr("calendar.anchorHelp")}</small>
            )}
          </label>
        );
      })}
      <p className="muted">{tr("calendar.replacementPreview")}</p>
    </fieldset>
  );
}
