import {
  dateNum,
  dateStr,
  type Calendar,
  type Project,
  type Task,
} from "./types";

export const MIN_PLANNING_DATE = "2000-01-01";
export const MAX_PLANNING_DATE = "2200-12-31";
export const MAX_DURATION_DAYS =
  dateNum(MAX_PLANNING_DATE) - dateNum(MIN_PLANNING_DATE) + 1;
export type PlanningCalendar = Calendar & {
  verified_years?: number[];
  ready?: boolean;
};
const yearKey = (value: string) =>
  /^\d{4}$/.test(value) && Number(value) >= 2000 && Number(value) <= 2200;

export function calendarPins(
  project: Project,
  calendars: Calendar[],
): Record<string, string> {
  const pins = { ...project.calendar_versions };
  const anchor = calendars.find(
    (calendar) => calendar.id === project.calendar_id,
  );
  if (anchor) pins[String(anchor.year)] = anchor.id;
  return pins;
}

export function changeCalendarAnchor(
  project: Project,
  id: string,
  calendars: Calendar[],
): Project {
  const anchor = calendars.find((calendar) => calendar.id === id);
  if (!anchor) return project;
  return {
    ...project,
    calendar_id: id,
    calendar_versions: {
      ...calendarPins(project, calendars),
      [String(anchor.year)]: id,
    },
  };
}

export function selectCalendarYear(
  project: Project,
  year: string,
  id: string,
  calendars: Calendar[],
): Project {
  if (!yearKey(year)) return project;
  const anchor = calendars.find(
    (calendar) => calendar.id === project.calendar_id,
  );
  const pins = calendarPins(project, calendars);
  if (!id) {
    if (String(anchor?.year) === year) return project;
    delete pins[year];
    return { ...project, calendar_versions: pins };
  }
  const selected = calendars.find(
    (calendar) => calendar.id === id && String(calendar.year) === year,
  );
  if (!selected) return project;
  return {
    ...project,
    calendar_id: String(anchor?.year) === year ? id : project.calendar_id,
    calendar_versions: { ...pins, [year]: id },
  };
}

export function calendarForProject(
  project: Project,
  calendars: Calendar[],
): PlanningCalendar {
  const pins = calendarPins(project, calendars);
  const anchor = calendars.find(
    (calendar) => calendar.id === project.calendar_id,
  );
  const days: Calendar["days"] = {};
  for (const [year, id] of Object.entries(pins)) {
    const calendar = calendars.find(
      (value) => value.id === id && String(value.year) === year,
    );
    if (calendar)
      for (const [day, value] of Object.entries(calendar.days))
        if (day.startsWith(year + "-")) days[day] = value;
  }
  const ready =
    !!anchor &&
    Object.entries(pins).every(([year, id]) =>
      calendars.some(
        (calendar) => calendar.id === id && String(calendar.year) === year,
      ),
    );
  return {
    id: project.calendar_id,
    year: anchor?.year || 0,
    title: anchor?.title || "",
    source: anchor?.source || "",
    checksum: anchor?.checksum || "",
    days,
    ready,
    verified_years: Object.keys(pins).filter(yearKey).map(Number),
  };
}

export function planningCalendarReady(
  calendar: PlanningCalendar | undefined,
): boolean {
  return !!calendar && calendar.ready !== false;
}

export function planningDay(
  calendar: PlanningCalendar | undefined,
  day: string,
): { working: boolean; shortened: boolean } | undefined {
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(day) ||
    day < MIN_PLANNING_DATE ||
    day > MAX_PLANNING_DATE
  )
    return undefined;
  const value = calendar?.days[day];
  if (value) return value;
  if (!planningCalendarReady(calendar)) return undefined;
  const year = Number(day.slice(0, 4));
  if (
    calendar?.verified_years?.includes(year) ||
    (!calendar?.verified_years && calendar?.year === year)
  )
    return undefined;
  const weekday = new Date(day + "T00:00:00Z").getUTCDay();
  return { working: weekday !== 0 && weekday !== 6, shortened: false };
}

export function resizedDuration(
  project: Project,
  task: Task,
  side: string | undefined,
  days: number,
  calendar?: PlanningCalendar,
): { requested_start: string; duration_days: number } {
  if (!planningCalendarReady(calendar))
    return {
      requested_start: task.requested_start,
      duration_days: task.duration_days,
    };
  const min = dateNum(MIN_PLANNING_DATE),
    max = dateNum(MAX_PLANNING_DATE);
  let start = Math.min(
    max,
    Math.max(min, dateNum(task.start || task.legacy_start || project.start)),
  );
  let end = Math.max(
    start,
    Math.min(max, dateNum(task.end || task.legacy_end || dateStr(start))),
  );
  if (side === "in") start = Math.max(min, Math.min(end, start + days));
  else end = Math.min(max, Math.max(start, end + days));
  let duration = 0;
  if (project.duration_mode === "calendar") duration = end - start + 1;
  else
    for (let day = start; day <= end; day++)
      if (planningDay(calendar, dateStr(day))?.working) duration++;
  return {
    requested_start: dateStr(start),
    duration_days: Math.max(1, duration),
  };
}
