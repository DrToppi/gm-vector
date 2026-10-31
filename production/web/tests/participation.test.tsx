import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PeoplePanel, TaskEditor } from "../src/Editors";
import { tr } from "../src/i18n";
import type { Project } from "../src/types";
import {
  Clock,
  employee,
  project,
  scenario,
  user,
  renderReady,
} from "./fixtures";

function weekday(container: HTMLElement, day: number) {
  const row = container.querySelector(`[data-weekday="${day}"]`)!;
  return {
    active: row.querySelector('input[type="checkbox"]')!,
    start: row.querySelectorAll('input[type="time"]')[0],
    end: row.querySelectorAll('input[type="time"]')[1],
    quota: row.querySelector('input[type="number"]') as HTMLInputElement,
  };
}

describe("per-project participation inputs", () => {
  const current = scenario();

  it("retains per-weekday quotas and date exceptions after a failed preview and retry", async () => {
    const f = current(),
      p = project();
    const changed = vi.fn(async (_project: Project) => false);
    const view = render(
      <Clock>
        <PeoplePanel
          project={p}
          employees={[employee]}
          admin={false}
          canEdit
          onChange={changed}
          onRefresh={async () => {}}
        />
      </Clock>,
    );
    fireEvent.click(screen.getByRole("button", { name: tr("mbf020f9374") }));
    const monday = weekday(view.container, 1),
      tuesday = weekday(view.container, 2);
    expect(monday.quota.value).toBe("4");
    fireEvent.change(monday.quota, { target: { value: "2" } });
    fireEvent.click(tuesday.active);
    fireEvent.change(tuesday.start, { target: { value: "10:00" } });
    fireEvent.change(tuesday.end, { target: { value: "14:00" } });
    fireEvent.change(tuesday.quota, { target: { value: "1.5" } });
    fireEvent.change(screen.getByLabelText(tr("m17af7907e9")), {
      target: { value: "2026-11-04" },
    });
    fireEvent.click(screen.getByRole("button", { name: tr("mc41bd2833e") }));
    const exception = view.container.querySelector(
      '[data-exception="2026-11-04"]',
    )!;
    fireEvent.click(exception.querySelector('input[type="checkbox"]')!);
    const times = exception.querySelectorAll('input[type="time"]');
    fireEvent.change(times[0], { target: { value: "09:30" } });
    fireEvent.change(times[1], { target: { value: "11:00" } });
    fireEvent.change(exception.querySelector('input[type="number"]')!, {
      target: { value: "1.5" },
    });
    fireEvent.submit(view.container.querySelector("form")!);
    await screen.findByRole("alert");
    expect(changed).toHaveBeenCalledTimes(1);
    const saved = changed.mock.calls[0][0];
    expect(saved.people[0].week?.["1"].daily_minutes).toBe(120);
    expect(saved.people[0].week?.["2"]).toEqual({
      active: true,
      start: "10:00",
      end: "14:00",
      daily_minutes: 90,
    });
    expect(saved.people[0].exceptions["2026-11-04"]).toEqual({
      active: true,
      start: "09:30",
      end: "11:00",
      daily_minutes: 90,
    });
    expect(monday.quota.value).toBe("2");
    expect(tuesday.quota.value).toBe("1.5");
    fireEvent.change(tuesday.quota, { target: { value: "1" } });
    fireEvent.submit(view.container.querySelector("form")!);
    await screen.findByRole("alert");
    expect(changed).toHaveBeenCalledTimes(2);
    expect(changed.mock.calls[1][0].people[0].week?.["2"].daily_minutes).toBe(
      60,
    );
    fireEvent.click(screen.getByRole("button", { name: tr("ma7a4033657") }));
    expect(view.container.querySelector("dialog")).toBeNull();
    expect(changed).toHaveBeenCalledTimes(2);
    expect(f.requests).toHaveLength(0);
  });

  it.each([
    { start: "13:00", end: "09:00", hours: "2", name: "reversed window" },
    { start: "09:00", end: "10:00", hours: "2", name: "quota beyond window" },
  ])(
    "rejects $name without proposing a project change",
    async ({ start, end, hours }) => {
      const changed = vi.fn(async (_project: Project) => false);
      const view = render(
        <Clock>
          <PeoplePanel
            project={project()}
            employees={[employee]}
            admin={false}
            canEdit
            onChange={changed}
            onRefresh={async () => {}}
          />
        </Clock>,
      );
      fireEvent.click(screen.getByRole("button", { name: tr("mbf020f9374") }));
      const monday = weekday(view.container, 1);
      fireEvent.change(monday.start, { target: { value: start } });
      fireEvent.change(monday.end, { target: { value: end } });
      fireEvent.change(monday.quota, { target: { value: hours } });
      fireEvent.submit(view.container.querySelector("form")!);
      expect(screen.getByRole("alert")).toBeTruthy();
      expect(changed).not.toHaveBeenCalled();
      expect(monday.quota.value).toBe(hours);
    },
  );

  it("cannot attach a new participant without an active configured day", async () => {
    const p = project();
    p.people = [];
    const changed = vi.fn(async (_project: Project) => true);
    const view = render(
      <Clock>
        <PeoplePanel
          project={p}
          employees={[employee]}
          admin={false}
          canEdit
          onChange={changed}
          onRefresh={async () => {}}
        />
      </Clock>,
    );
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: employee.id },
    });
    fireEvent.click(screen.getByRole("button", { name: tr("mb50e40a352") }));
    fireEvent.submit(view.container.querySelector("form")!);
    expect(screen.getByRole("alert").textContent).toContain(tr("m066ff0a965"));
    expect(changed).not.toHaveBeenCalled();
    expect(p.people).toEqual([]);
  });

  it.each([
    { daily: 180, available: true },
    { daily: 0, available: false },
  ])(
    "defaults task assignment to the global/project/window minimum, global=$daily",
    async ({ daily, available }) => {
      const f = current(),
        p = project(),
        e = { ...employee, daily_minutes: daily };
      f.sidecars();
      p.tasks[0].assignments = [];
      p.people[0].week = {
        "1": { active: true, start: "09:00", end: "10:00", daily_minutes: 120 },
      };
      p.people[0].exceptions = {
        "2026-11-04": {
          active: true,
          start: "09:00",
          end: "15:00",
          daily_minutes: 240,
        },
      };
      const save = vi.fn();
      const view = await renderReady(
        <Clock>
          <TaskEditor
            task={p.tasks[0]}
            project={p}
            employees={[e]}
            user={user}
            canManage
            canWrite
            onSave={save}
            onDelete={vi.fn()}
            onChild={vi.fn()}
            onClose={vi.fn()}
            onRefresh={async () => {}}
          />
        </Clock>,
      );
      const assignment = screen.getByRole("checkbox", {
        name: tr("m23a0d6bee7") + e.name,
      }) as HTMLInputElement;
      expect(assignment.disabled).toBe(!available);
      if (available) {
        fireEvent.click(assignment);
        fireEvent.submit(view.container.querySelector("form")!);
        expect(save).toHaveBeenCalledWith(
          expect.objectContaining({
            assignments: [{ employee_id: e.id, daily_minutes: 180 }],
          }),
        );
      }
    },
  );
});
