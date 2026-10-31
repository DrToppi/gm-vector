import type { TimeEntry } from "./types";
export type HistoryFilters = {
  task_id: string;
  employee_id: string;
  date_from: string;
  date_to: string;
};
export type HistoryItem = TimeEntry & {
  task_title: string;
  employee_name: string;
};
export type HistoryCommon = {
  project_id: string;
  project_title: string;
  revision: number;
  planning_timezone: string;
  filters: HistoryFilters;
  order: "work_date_desc_id_asc";
  total_count: number;
  recorded_minutes: number;
  legacy_unattributed: {
    minutes: number;
    scope: "project" | "task_subtree";
    employee_date_attribution: "unavailable";
  };
};
export type HistoryPage = HistoryCommon & {
  limit: number;
  items: HistoryItem[];
  next_cursor: string | null;
};
export type HistoryExport = HistoryCommon & {
  format: "gm-vector-time-history";
  schema_version: 1;
  generated_at: string;
  items: HistoryItem[];
};
export const historyFilters = (task_id = ""): HistoryFilters => ({
  task_id,
  employee_id: "",
  date_from: "",
  date_to: "",
});
export function historyQuery(
  filters: HistoryFilters,
  cursor: string | null = null,
  limit = 100,
) {
  return new URLSearchParams({
    ...filters,
    limit: String(limit),
    ...(cursor ? { cursor } : {}),
  }).toString();
}
export const historyBase = (projectID: string) =>
  "/projects/" + encodeURIComponent(projectID) + "/time-history";
export function canDeleteHistory(
  canWrite: boolean,
  canManage: boolean,
  employeeID: string,
  ownerID: string,
) {
  return canWrite && (canManage || (!!employeeID && employeeID === ownerID));
}
