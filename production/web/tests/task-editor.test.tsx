import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TaskEditor } from "../src/Editors";
import { tr } from "../src/i18n";
import {
  deferred,
  employee,
  historyPage,
  project,
  reply,
  scenario,
  user,
  Clock,
  renderReady,
} from "./fixtures";

describe("task draft and effort ownership", () => {
  const current = scenario();
  function props(p = project()) {
    return {
      task: p.tasks[0],
      project: p,
      employees: [employee],
      user,
      canManage: true,
      canWrite: true,
      onSave: vi.fn(),
      onDelete: vi.fn(),
      onChild: vi.fn(),
      onClose: vi.fn(),
      onRefresh: vi.fn(async () => {}),
    };
  }

  it("keeps title and plan drafts while a real effort POST refreshes authoritative actual hours", async () => {
    const f = current();
    f.sidecars();
    f.hold("POST", "/projects/A/time-entries");
    const original = project(),
      controls = props(original);
    const refreshed = deferred<void>();
    controls.onRefresh.mockImplementation(() => refreshed.promise);
    const view = await renderReady(
      <Clock>
        <TaskEditor {...controls} />
      </Clock>,
    );
    fireEvent.change(screen.getByLabelText(tr("m0918b4ba92")), {
      target: { value: "Unsaved title draft" },
    });
    fireEvent.change(screen.getByLabelText(tr("m455768fd15")), {
      target: { value: "4" },
    });
    fireEvent.click(screen.getByRole("button", { name: tr("m8d6ba6bac1") }));
    const post = f.latest("POST", "/projects/A/time-entries");
    expect(post.body).toMatchObject({
      task_id: "task-1",
      employee_id: employee.id,
      minutes: 60,
    });
    expect(post.headers.get("X-CSRF-Token")).toBe("synthetic-csrf");
    await reply(
      post,
      {
        id: "new-fact",
        task_id: "task-1",
        employee_id: employee.id,
        minutes: 60,
        date: "2026-11-02",
        comment: "",
      },
      201,
    );
    expect(controls.onRefresh).toHaveBeenCalledTimes(1);
    const updated = structuredClone(original);
    updated.revision = 2;
    updated.tasks[0].actual_minutes = 60;
    f.auto(
      "GET",
      "/projects/A/time-history",
      historyPage("A", { revision: 2, items: [] }),
    );
    await act(async () => {
      view.rerender(
        <Clock>
          <TaskEditor {...controls} project={updated} task={updated.tasks[0]} />
        </Clock>,
      );
      refreshed.resolve(undefined);
    });
    expect(
      (screen.getByLabelText(tr("m0918b4ba92")) as HTMLInputElement).value,
    ).toBe("Unsaved title draft");
    expect(
      (screen.getByLabelText(tr("m455768fd15")) as HTMLInputElement).value,
    ).toBe("4");
    expect(
      view.container.querySelector("[data-task-fact]")?.textContent,
    ).toContain(tr("mc9a521ce2e") + " 1");
    fireEvent.submit(view.container.querySelector("form")!);
    expect(controls.onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Unsaved title draft",
        planned_minutes: 240,
      }),
    );
  });

  it("preserves a failed-save draft for retry and closes without a cancellation mutation", async () => {
    const f = current();
    f.sidecars();
    const controls = props();
    const view = await renderReady(
      <Clock>
        <TaskEditor {...controls} />
      </Clock>,
    );
    fireEvent.change(screen.getByLabelText(tr("m0918b4ba92")), {
      target: { value: "Retry this title" },
    });
    fireEvent.submit(view.container.querySelector("form")!);
    view.rerender(
      <Clock>
        <TaskEditor {...controls} error="Synthetic preview rejected" />
      </Clock>,
    );
    expect(screen.getByRole("alert").textContent).toBe(
      "Synthetic preview rejected",
    );
    expect(
      (screen.getByLabelText(tr("m0918b4ba92")) as HTMLInputElement).value,
    ).toBe("Retry this title");
    fireEvent.submit(view.container.querySelector("form")!);
    expect(controls.onSave).toHaveBeenCalledTimes(2);
    const dialog = view.container.querySelector("dialog")!;
    fireEvent.click(dialog);
    const cancel = new Event("cancel", { cancelable: true });
    fireEvent(dialog, cancel);
    expect(cancel.defaultPrevented).toBe(true);
    expect(controls.onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: tr("ma7a4033657") }));
    expect(controls.onClose).toHaveBeenCalledTimes(1);
    expect(f.requests.filter((request) => request.method !== "GET")).toEqual(
      [],
    );
  });

  it("refuses header close and duplicate effort submission while the mutation is held", async () => {
    const f = current();
    f.sidecars();
    f.hold("POST", "/projects/A/time-entries");
    const controls = props();
    await renderReady(
      <Clock>
        <TaskEditor {...controls} />
      </Clock>,
    );
    const effortSubmit = screen.getByRole("button", {
      name: tr("m8d6ba6bac1"),
    });
    fireEvent.change(screen.getByLabelText(tr("m80492ccc34")), {
      target: { value: "Keep this rejected fact draft" },
    });
    fireEvent.click(effortSubmit);
    fireEvent.click(screen.getByRole("button", { name: tr("ma7a4033657") }));
    expect(controls.onClose).not.toHaveBeenCalled();
    expect(
      (
        screen.getByRole("button", {
          name: tr("md5c911ec8d"),
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.submit(effortSubmit.closest("form")!);
    expect(f.find("POST", "/projects/A/time-entries")).toHaveLength(1);
    await reply(
      f.latest("POST", "/projects/A/time-entries"),
      { error: { code: "validation", message: "Synthetic effort rejection" } },
      422,
    );
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(
      (screen.getByLabelText(tr("m80492ccc34")) as HTMLTextAreaElement).value,
    ).toBe("Keep this rejected fact draft");
    fireEvent.click(screen.getByRole("button", { name: tr("ma7a4033657") }));
    expect(controls.onClose).toHaveBeenCalledTimes(1);
  });

  it.each([false, true])(
    "preserves viewer/member property restrictions, writable=%s",
    async (canWrite) => {
      const f = current();
      f.sidecars();
      const controls = { ...props(), canManage: false, canWrite };
      const view = await renderReady(
        <Clock>
          <TaskEditor {...controls} />
        </Clock>,
      );
      expect(
        (screen.getByLabelText(tr("m0918b4ba92")) as HTMLInputElement).disabled,
      ).toBe(true);
      expect(
        (screen.getByLabelText(tr("m455768fd15")) as HTMLInputElement).disabled,
      ).toBe(true);
      expect(
        (screen.getByLabelText(tr("m225077c6d4")) as HTMLSelectElement)
          .disabled,
      ).toBe(!canWrite);
      expect(
        screen.queryByRole("button", { name: tr("mbe99b13612") }),
      ).toBeNull();
      if (!canWrite) {
        fireEvent.submit(view.container.querySelector("form")!);
        expect(controls.onSave).not.toHaveBeenCalled();
        expect(f.requests.every((request) => request.method === "GET")).toBe(
          true,
        );
      }
    },
  );
});
