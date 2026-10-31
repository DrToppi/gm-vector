import { tr } from "./i18n";
import type { Activity, Participation, Employee } from "./types";
export const weekdays = [1, 2, 3, 4, 5, 6, 0];
export const weekdayNames = () => [
  tr("m7971972c7c"),
  tr("m31a8ee2fb6"),
  tr("m839b2d5c5a"),
  tr("m7ec16b9e41"),
  tr("m956618723d"),
  tr("m4ca114c23b"),
  tr("m95dab017a4"),
];
export const emptyActivity = (): Activity => ({
  active: false,
  start: "",
  end: "",
  daily_minutes: 0,
});
export const hasParticipation = (v: Participation) =>
  Object.keys(v.week || {}).length > 0;
export function activityCapacity(v: Activity): number {
  const minutes = (s: string) => {
    const m = /^(\d{2}):(\d{2})$/.exec(s);
    return m ? Number(m[1]) * 60 + Number(m[2]) : NaN;
  };
  const span = minutes(v.end) - minutes(v.start);
  return v.active && span > 0 && Number(v.daily_minutes) > 0
    ? Math.min(span, Number(v.daily_minutes))
    : 0;
}
export function participationLimit(
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
