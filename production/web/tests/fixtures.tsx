import {
  act,
  cleanup,
  render,
  type RenderResult,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, vi } from "vitest";
import type { ReactNode } from "react";
import { advanceSession, setCSRF } from "../src/api";
import { changeLocale } from "../src/i18n";
import { PlanningClockProvider } from "../src/PlanningClock";
import {
  historyFilters,
  type HistoryItem,
  type HistoryPage,
  type HistoryExport,
} from "../src/history";
import {
  blankTask,
  type Project,
  type Employee,
  type User,
} from "../src/types";

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

export const employee: Employee = {
  id: "employee-1",
  name: "Example Employee",
  daily_minutes: 480,
  user_id: "user-1",
  provider: "",
  external_id: "",
};
export const user: User = {
  id: "user-1",
  username: "component-user",
  name: "Example User",
  admin: false,
  active: true,
  employee_id: employee.id,
};

export function project(id = "A"): Project {
  return {
    id,
    title: "Project " + id,
    brief: "Synthetic component fixture",
    start: "2026-11-02",
    duration_mode: "working",
    calendar_id: "calendar-2026",
    calendar_versions: {},
    revision: 1,
    external_reference: "",
    calculation_state: "ready",
    tasks: [
      {
        ...blankTask("", "2026-11-02"),
        id: "task-1",
        title: "Original task",
        planned_minutes: 480,
        start: "2026-11-02",
        end: "2026-11-03",
        assignments: [{ employee_id: employee.id, daily_minutes: 240 }],
      },
    ],
    links: [],
    people: [
      {
        employee_id: employee.id,
        weekdays: [1, 2, 3, 4, 5],
        window_start: "09:00",
        window_end: "17:00",
        week: {
          "1": {
            active: true,
            start: "09:00",
            end: "13:00",
            daily_minutes: 240,
          },
        },
        exceptions: {},
      },
    ],
  };
}

export function historyItem(
  id = "entry-1",
  overrides: Partial<HistoryItem> = {},
): HistoryItem {
  return {
    id,
    task_id: "task-1",
    task_title: "Original task",
    employee_id: employee.id,
    employee_name: employee.name,
    date: "2026-11-02",
    minutes: 60,
    comment: "Synthetic fact",
    ...overrides,
  };
}
export function historyPage(
  id = "A",
  overrides: Partial<HistoryPage> = {},
): HistoryPage {
  return {
    project_id: id,
    project_title: "Project " + id,
    revision: 1,
    planning_timezone: "Europe/Moscow",
    filters: historyFilters("task-1"),
    order: "work_date_desc_id_asc",
    total_count: 1,
    recorded_minutes: 60,
    legacy_unattributed: {
      minutes: 0,
      scope: "task_subtree",
      employee_date_attribution: "unavailable",
    },
    limit: 100,
    items: [historyItem()],
    next_cursor: null,
    ...overrides,
  };
}
export function historyExport(
  id = "A",
  overrides: Partial<HistoryExport> = {},
): HistoryExport {
  const { limit: _limit, next_cursor: _next, ...common } = historyPage(id);
  return {
    ...common,
    format: "gm-vector-time-history",
    schema_version: 1,
    generated_at: "2026-11-02T09:00:00Z",
    ...overrides,
  };
}
export function Clock({ children }: { children: ReactNode }) {
  return (
    <PlanningClockProvider zone="Europe/Moscow">
      {children}
    </PlanningClockProvider>
  );
}

export type RequestControl = {
  method: string;
  url: URL;
  path: string;
  body: unknown;
  headers: Headers;
  signal?: AbortSignal | null;
  settled: boolean;
  reply: (body: unknown, status?: number) => void;
  replyHeaders: (status?: number) => void;
  replyBody: (body: unknown) => void;
  reject: (reason: unknown) => void;
};
type Match = string | ((request: RequestControl) => boolean);
type Route = {
  method: string;
  match: Match;
  reply?: (request: RequestControl) => unknown;
};

export class FetchFixture {
  requests: RequestControl[] = [];
  unexpected: string[] = [];
  private routes: Route[] = [];
  constructor() {
    // Intentionally ignore AbortSignal: passing these tests requires the real
    // session/context guards, not merely a cooperative transport.
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const response = deferred<Response>(),
          body = deferred<unknown>();
        const url = new URL(String(input), location.origin);
        let sentHeaders = false;
        const request: RequestControl = {
          method: init?.method || "GET",
          url,
          path: url.pathname.replace(/^\/api\/v1/, ""),
          body: init?.body ? JSON.parse(String(init.body)) : undefined,
          headers: new Headers(init?.headers),
          signal: init?.signal,
          settled: false,
          replyHeaders: (status = 200) => {
            if (sentHeaders) throw Error("Fixture sent response headers twice");
            sentHeaders = true;
            response.resolve({
              ok: status >= 200 && status < 300,
              status,
              headers: new Headers({ "X-Request-ID": "synthetic-request" }),
              json: () => body.promise,
            } as Response);
          },
          replyBody: (value) => {
            if (request.settled)
              throw Error("Fixture completed response twice");
            request.settled = true;
            body.resolve(structuredClone(value));
          },
          reply: (value, status = 200) => {
            request.replyHeaders(status);
            request.replyBody(value);
          },
          reject: (reason) => {
            request.settled = true;
            if (sentHeaders) body.reject(reason);
            else response.reject(reason);
          },
        };
        this.requests.push(request);
        const route = [...this.routes]
          .reverse()
          .find(
            (route) =>
              route.method === request.method &&
              (typeof route.match === "string"
                ? route.match === request.path
                : route.match(request)),
          );
        if (!route) {
          this.unexpected.push(request.method + " " + request.path);
          request.reject(Error("Unexpected synthetic request"));
        } else if (route.reply) request.reply(route.reply(request));
        return response.promise;
      }),
    );
  }
  hold(method: string, match: Match) {
    this.routes.push({ method, match });
  }
  auto(
    method: string,
    match: Match,
    reply: unknown | ((request: RequestControl) => unknown),
  ) {
    this.routes.push({
      method,
      match,
      reply:
        typeof reply === "function"
          ? (reply as (request: RequestControl) => unknown)
          : () => reply,
    });
  }
  find(method: string, path: string) {
    return this.requests.filter(
      (request) => request.method === method && request.path === path,
    );
  }
  latest(method: string, path: string) {
    const request = this.find(method, path).at(-1);
    if (!request)
      throw Error("Missing synthetic request: " + method + " " + path);
    return request;
  }
  sidecars(id = "A") {
    this.auto(
      "GET",
      "/projects/" + id + "/time-history",
      historyPage(id, { total_count: 0, recorded_minutes: 0, items: [] }),
    );
    this.auto("GET", "/projects/" + id + "/tasks/task-1/comments", {
      comments: [],
      email_enabled: false,
      can_comment: false,
      customer_email: "",
      customer: false,
    });
  }
}

// This is only a DOM adapter. Native focus, Tab/Escape and browser modality are
// deliberately established by the real-browser suite, not by this shim.
function dialogAdapter() {
  const proto = HTMLDialogElement.prototype;
  const show = Object.getOwnPropertyDescriptor(proto, "showModal");
  const close = Object.getOwnPropertyDescriptor(proto, "close");
  Object.defineProperty(proto, "showModal", {
    configurable: true,
    value() {
      this.setAttribute("open", "");
    },
  });
  Object.defineProperty(proto, "close", {
    configurable: true,
    value() {
      this.removeAttribute("open");
    },
  });
  return () => {
    if (show) Object.defineProperty(proto, "showModal", show);
    else Reflect.deleteProperty(proto, "showModal");
    if (close) Object.defineProperty(proto, "close", close);
    else Reflect.deleteProperty(proto, "close");
  };
}

export function scenario() {
  let fixture: FetchFixture;
  let restoreDialog: () => void;
  beforeEach(async () => {
    advanceSession();
    setCSRF("synthetic-csrf");
    history.replaceState(null, "", "/");
    await changeLocale("en");
    fixture = new FetchFixture();
    restoreDialog = dialogAdapter();
  });
  afterEach(async () => {
    const pending = fixture.requests.filter((request) => !request.settled);
    await act(async () => {
      cleanup();
      advanceSession();
      for (const request of pending)
        request.reject(new DOMException("Fixture cleanup", "AbortError"));
    });
    restoreDialog();
    expect(
      fixture.unexpected,
      "Unexpected API route escaped the synthetic fixture",
    ).toEqual([]);
    expect(
      pending.map((request) => request.method + " " + request.path),
      "Test left a held request unsettled",
    ).toEqual([]);
  });
  return () => fixture;
}

export async function reply(
  request: RequestControl,
  body: unknown,
  status = 200,
) {
  await act(async () => request.reply(body, status));
}

export async function renderReady(element: ReactNode) {
  let view!: RenderResult;
  await act(async () => {
    view = render(element);
  });
  return view;
}
