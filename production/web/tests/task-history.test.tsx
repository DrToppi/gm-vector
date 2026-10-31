import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TaskHistory } from "../src/TaskHistory";
import { tr } from "../src/i18n";
import {
  employee,
  historyItem,
  historyPage,
  reply,
  scenario,
  user,
} from "./fixtures";

describe("authorized task history lifecycle", () => {
  const current = scenario();
  const path = "/projects/A/time-history";
  const props = {
    projectID: "A",
    taskID: "task-1",
    revision: 1,
    employees: [employee],
    user,
    canWrite: true,
    canManage: false,
    reload: 0,
    onDelete: async () => {},
  };

  it("normalizes a changed revision with exactly one first-page fetch", async () => {
    const f = current();
    f.hold("GET", path);
    const view = render(<TaskHistory {...props} />);
    await reply(f.latest("GET", path), historyPage());
    expect(screen.getByText("Synthetic fact", { exact: false })).toBeTruthy();
    view.rerender(<TaskHistory {...props} revision={2} />);
    expect(f.find("GET", path)).toHaveLength(2);
    await reply(
      f.latest("GET", path),
      historyPage("A", {
        revision: 2,
        items: [historyItem("revision-2", { comment: "Updated revision" })],
      }),
    );
    expect(screen.getByText("Updated revision", { exact: false })).toBeTruthy();
    expect(f.find("GET", path)).toHaveLength(2);
    expect(f.latest("GET", path).url.searchParams.get("cursor")).toBeNull();
  });

  it("keeps the original continuation when page one is read again", async () => {
    const f = current();
    f.hold("GET", path);
    render(<TaskHistory {...props} />);
    await reply(
      f.latest("GET", path),
      historyPage("A", { next_cursor: "original-fixed-expiry" }),
    );
    fireEvent.click(screen.getByRole("button", { name: tr("history.next") }));
    expect(f.latest("GET", path).url.searchParams.get("cursor")).toBe(
      "original-fixed-expiry",
    );
    await reply(
      f.latest("GET", path),
      historyPage("A", {
        items: [historyItem("second-page", { comment: "Second page" })],
        next_cursor: null,
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: tr("history.previous") }),
    );
    await reply(
      f.latest("GET", path),
      historyPage("A", {
        next_cursor: "a-newly-issued-token-must-not-replace-original",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: tr("history.next") }));
    expect(f.latest("GET", path).url.searchParams.get("cursor")).toBe(
      "original-fixed-expiry",
    );
    await reply(f.latest("GET", path), historyPage("A", { items: [] }));
    expect(f.find("GET", path)).toHaveLength(4);
  });

  it("rejects an obsolete response and restarts explicitly after cursor expiry", async () => {
    const f = current();
    f.hold("GET", path);
    f.hold("GET", "/projects/B/time-history");
    const view = render(<TaskHistory {...props} />);
    const old = f.latest("GET", path);
    view.rerender(<TaskHistory {...props} projectID="B" />);
    expect(old.signal?.aborted).toBe(true);
    await reply(
      f.latest("GET", "/projects/B/time-history"),
      historyPage("B", {
        items: [historyItem("B-entry", { comment: "Only project B" })],
        next_cursor: "expired-token",
      }),
    );
    await reply(
      old,
      historyPage("A", {
        items: [historyItem("late-A", { comment: "Obsolete private A" })],
      }),
    );
    expect(
      screen.queryByText("Obsolete private A", { exact: false }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: tr("history.next") }));
    await reply(
      f.latest("GET", "/projects/B/time-history"),
      {
        error: { code: "cursor_expired", message: "Synthetic expired cursor" },
      },
      422,
    );
    expect(screen.getByRole("alert").textContent).toContain(
      tr("history.expired"),
    );
    const before = f.requests.length;
    expect(
      screen.getByRole("button", { name: tr("history.restart") }),
    ).toBeTruthy();
    expect(f.requests).toHaveLength(before);
    fireEvent.click(
      screen.getByRole("button", { name: tr("history.restart") }),
    );
    expect(
      f
        .latest("GET", "/projects/B/time-history")
        .url.searchParams.get("cursor"),
    ).toBeNull();
    await reply(
      f.latest("GET", "/projects/B/time-history"),
      historyPage("B", { items: [] }),
    );
  });

  it("retains a selected former employee and excludes NULL ownership", async () => {
    const f = current();
    f.hold("GET", path);
    render(<TaskHistory {...props} user={{ ...user, employee_id: "" }} />);
    await reply(
      f.latest("GET", path),
      historyPage("A", {
        items: [
          historyItem("former", {
            employee_id: "former-employee",
            employee_name: "Former Example",
          }),
          historyItem("unattributed", { employee_id: "", employee_name: "" }),
        ],
      }),
    );
    const select = screen.getByRole("combobox", {
      name: tr("history.employeeFilter"),
    }) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "former-employee" } });
    fireEvent.click(screen.getByRole("button", { name: tr("history.apply") }));
    expect(f.latest("GET", path).url.searchParams.get("employee_id")).toBe(
      "former-employee",
    );
    await reply(
      f.latest("GET", path),
      historyPage("A", { items: [], total_count: 0, recorded_minutes: 0 }),
    );
    expect(select.value).toBe("former-employee");
    expect(screen.getByRole("option", { name: "Former Example" })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: tr("mbe99b13612") }),
    ).toBeNull();
  });

  it("renders long user text literally rather than treating it as markup", async () => {
    const f = current();
    f.hold("GET", path);
    const view = render(<TaskHistory {...props} />);
    const literal =
      '<img src=x onerror="unexpected()">' + "long-note-".repeat(200);
    await reply(
      f.latest("GET", path),
      historyPage("A", {
        items: [historyItem("literal-text", { comment: literal })],
      }),
    );
    expect(
      view.container.querySelector('[data-time-entry="literal-text"]')
        ?.textContent,
    ).toContain(literal);
    expect(view.container.querySelector("img")).toBeNull();
  });
});
