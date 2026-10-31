import { tr } from "./i18n";
import { COMPATIBILITY_ZONE, planningToday } from "./planningTime";
export type Assignment = { employee_id: string; daily_minutes: number };
export type Task = {
  duration_days: number;
  id: string;
  parent: string;
  title: string;
  description: string;
  result: string;
  brief: string;
  status: "todo" | "doing" | "done";
  requested_start: string;
  start: string;
  end: string;
  planned_minutes: number;
  actual_minutes: number;
  legacy_actual_minutes: number;
  responsibility: "internal" | "customer";
  customer_owner: string;
  customer_email?: string;
  customer_due: string;
  owner_legacy: string;
  legacy_start: string;
  legacy_end: string;
  assignments: Assignment[];
  summary: boolean;
  blocked: boolean;
};
export type Link = {
  from: string;
  to: string;
  mode: "together" | "after" | "delay";
  days: number;
};
export type Activity = {
  active: boolean;
  start: string;
  end: string;
  daily_minutes?: number;
};
export type Participation = {
  employee_id: string;
  weekdays: number[];
  window_start: string;
  window_end: string;
  exceptions: Record<string, Activity>;
  week?: Record<string, Activity>;
};
export type Project = {
  calculation_state?: "ready" | "needs_participation" | "blocked_capacity";
  id: string;
  title: string;
  brief: string;
  start: string;
  duration_mode: "working" | "calendar";
  calendar_id: string;
  calendar_versions?: Record<string, string>;
  provisional_years?: number[];
  revision: number;
  external_reference: string;
  tasks: Task[];
  links: Link[];
  people: Participation[];
  warnings?: string[];
  loads?: {
    employee_id: string;
    task_id: string;
    date: string;
    minutes: number;
  }[];
};
export type Employee = {
  email?: string;
  id: string;
  name: string;
  daily_minutes: number;
  user_id: string;
  provider: string;
  external_id: string;
};
export type Calendar = {
  id: string;
  year: number;
  title: string;
  source: string;
  checksum: string;
  days: Record<string, { working: boolean; shortened: boolean }>;
};
export type User = {
  locale?: string;
  id: string;
  username: string;
  name: string;
  admin: boolean;
  active: boolean;
  employee_id: string;
};
export type TimeEntry = {
  id: string;
  task_id: string;
  employee_id: string;
  date: string;
  minutes: number;
  comment: string;
};
export const statuses = {
  get todo() {
    return tr("mc6cc849619");
  },
  get doing() {
    return tr("m59a28f1b11");
  },
  get done() {
    return tr("mef05d57959");
  },
};
export const dateNum = (s: string) =>
  Math.floor(Date.parse(s + "T00:00:00Z") / 86400000);
export const dateStr = (n: number) =>
  new Date(n * 86400000).toISOString().slice(0, 10);
export const today = (zone = COMPATIBILITY_ZONE) => planningToday(zone);
export const hours = (n: number) => Number((n / 60).toFixed(2));
export const clone = <T>(v: T): T => structuredClone(v);
export function blankTask(parent = "", start = today()): Task {
  return {
    duration_days: 0,
    id: crypto.randomUUID(),
    parent,
    title: tr("m756c6c64e2"),
    description: "",
    result: "",
    brief: "",
    status: "todo",
    requested_start: start,
    start: "",
    end: "",
    planned_minutes: 0,
    actual_minutes: 0,
    legacy_actual_minutes: 0,
    responsibility: "internal",
    customer_owner: "",
    customer_email: "",
    customer_due: "",
    owner_legacy: "",
    legacy_start: "",
    legacy_end: "",
    assignments: [],
    summary: false,
    blocked: false,
  };
}
