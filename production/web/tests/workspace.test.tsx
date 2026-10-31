import {
  act,
  fireEvent,
  renderHook,
  screen,
  type RenderHookResult,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useWorkspaceController } from "../src/useWorkspaceController";
import { Workspace } from "../src/Workspace";
import { advanceSession } from "../src/api";
import {
  Clock,
  calendar,
  employee,
  project,
  renderReady,
  reply,
  scenario,
  user,
  type FetchFixture,
} from "./fixtures";

type Controller = ReturnType<typeof useWorkspaceController>;
function workspaceRoutes(f: FetchFixture) {
  f.auto(
    "GET",
    "/projects",
    ["A", "B"].map((id) => ({ id, title: "Project " + id, task_count: 1 })),
  );
  f.auto("GET", "/employees", [employee]);
  f.auto("GET", "/calendars", [calendar()]);
  f.auto("GET", "/projects/A", { project: project("A"), role: "manager" });
  f.auto("GET", "/projects/B", { project: project("B"), role: "manager" });
}
async function mountController(f: FetchFixture) {
  workspaceRoutes(f);
  const props = { user, onLogout: vi.fn() };
  let hook!: RenderHookResult<Controller, unknown>;
  await act(async () => {
    hook = renderHook(() => useWorkspaceController(props), { wrapper: Clock });
  });
  expect(hook.result.current.project?.id).toBe("A");
  return hook;
}

describe("real workspace coordination and view boundary", () => {
  const current = scenario();

  it("keeps the new A through A→B→A and rejects callbacks from the former A generation", async () => {
    const f = current(),
      hook = await mountController(f);
    const obsolete = hook.result.current;
    f.hold("GET", "/projects/A");
    f.hold("GET", "/projects/B");
    let oldRead!: Promise<void>;
    await act(async () => {
      oldRead = hook.result.current.loadProject("A");
    });
    const oldA = f.latest("GET", "/projects/A");
    let openB!: Promise<void>;
    await act(async () => {
      openB = hook.result.current.open("B");
    });
    await reply(f.latest("GET", "/projects/B"), {
      project: project("B"),
      role: "manager",
    });
    await openB;
    expect(hook.result.current.project?.id).toBe("B");
    let openA!: Promise<void>;
    await act(async () => {
      openA = hook.result.current.open("A");
    });
    const fresh = { ...project("A"), revision: 3, title: "Latest A" };
    await reply(f.latest("GET", "/projects/A"), {
      project: fresh,
      role: "viewer",
    });
    await openA;
    expect(oldA.signal?.aborted).toBe(true);
    await reply(oldA, {
      project: { ...project("A"), title: "Obsolete A" },
      role: "manager",
    });
    await oldRead;
    expect(hook.result.current.project?.title).toBe("Latest A");
    expect(hook.result.current.canManage).toBe(false);
    const before = f.requests.length;
    await act(async () => {
      await obsolete.refreshLists();
      await obsolete.loadProject("A");
      expect(
        await obsolete.propose({ ...fresh, title: "Obsolete proposal" }),
      ).toBe(false);
      await obsolete.saveGesture(fresh);
      await obsolete.archiveProject();
      await obsolete.exportProject("json");
    });
    expect(f.requests).toHaveLength(before);
    expect(hook.result.current.project?.title).toBe("Latest A");
    hook.unmount();
  });

  it("does not apply a completed archive to a subsequently selected project", async () => {
    const f = current(),
      hook = await mountController(f);
    f.hold("DELETE", "/projects/A");
    let archive!: Promise<void>;
    await act(async () => {
      archive = hook.result.current.archiveProject();
    });
    const deletion = f.latest("DELETE", "/projects/A");
    expect(deletion.body).toEqual({ revision: 1 });
    await act(async () => {
      await hook.result.current.open("B");
    });
    const before = f.requests.length;
    await reply(deletion, { ok: true });
    await archive;
    expect(hook.result.current.project?.id).toBe("B");
    expect(f.requests).toHaveLength(before);
    expect(hook.result.current.error).toBe("");
    hook.unmount();
  });

  it("discards a held catalog response after a same-project selection generation changes", async () => {
    const f = current(),
      hook = await mountController(f);
    f.hold("GET", "/projects");
    let lists!: Promise<void>;
    await act(async () => {
      lists = hook.result.current.refreshLists();
    });
    const oldList = f.latest("GET", "/projects");
    await act(async () => {
      await hook.result.current.open("A");
    });
    expect(oldList.signal?.aborted).toBe(true);
    await reply(oldList, []);
    await lists;
    expect(hook.result.current.project?.id).toBe("A");
    expect(hook.result.current.projects.map((p) => p.id)).toEqual(["A", "B"]);
    expect(hook.result.current.employees).toEqual([employee]);
    hook.unmount();
  });

  it("polls after completion, pauses for drafts, and tears down timers and listeners", async () => {
    vi.useFakeTimers();
    vi.spyOn(document, "hidden", "get").mockReturnValue(false);
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(true);
    const f = current(),
      hook = await mountController(f);
    f.hold("GET", "/projects/A");
    const baseline = f.find("GET", "/projects/A").length;
    await act(async () => vi.advanceTimersByTimeAsync(5000));
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 1);
    await act(async () => vi.advanceTimersByTimeAsync(50000));
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 1);
    await reply(f.latest("GET", "/projects/A"), {
      project: { ...project(), revision: 2 },
      role: "manager",
    });
    await act(async () => vi.advanceTimersByTimeAsync(4999));
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 1);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 2);
    await reply(f.latest("GET", "/projects/A"), {
      project: { ...project(), revision: 3 },
      role: "manager",
    });
    await act(async () => {
      hook.result.current.setTaskEditor(project().tasks[0]);
    });
    await act(async () => vi.advanceTimersByTimeAsync(10000));
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 2);
    await act(async () => {
      hook.result.current.setTaskEditor(null);
    });
    expect(f.find("GET", "/projects/A")).toHaveLength(baseline + 3);
    await reply(f.latest("GET", "/projects/A"), {
      project: { ...project(), revision: 4 },
      role: "manager",
    });
    hook.unmount();
    const end = f.requests.length;
    fireEvent(window, new Event("online"));
    fireEvent(document, new Event("visibilitychange"));
    await act(async () => vi.advanceTimersByTimeAsync(50000));
    expect(f.requests).toHaveLength(end);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("cannot use an old controller callback after session replacement", async () => {
    const f = current(),
      hook = await mountController(f);
    const old = hook.result.current;
    f.hold("GET", "/projects/A");
    let reading!: Promise<void>;
    await act(async () => {
      reading = hook.result.current.loadProject("A");
    });
    const pending = f.latest("GET", "/projects/A");
    await act(async () => {
      advanceSession();
    });
    expect(pending.signal?.aborted).toBe(true);
    hook.unmount();
    await reply(pending, {
      project: { ...project(), title: "Old session data" },
      role: "manager",
    });
    await reading;
    const before = f.requests.length;
    await old.loadProject("A");
    await old.refreshLists();
    expect(await old.propose(project())).toBe(false);
    expect(f.requests).toHaveLength(before);
  });

  it("renders the extracted Workspace through its real controller and navigation", async () => {
    const f = current();
    workspaceRoutes(f);
    f.auto("GET", "/diagnostics/key", { configured: false });
    class Observer {
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal("ResizeObserver", Observer);
    const view = await renderReady(
      <Clock>
        <Workspace user={user} onLogout={vi.fn()} />
      </Clock>,
    );
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Project A",
    );
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Project B/ }));
    });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Project B",
    );
    expect(f.find("GET", "/projects/B")).toHaveLength(1);
    view.unmount();
  });
});
