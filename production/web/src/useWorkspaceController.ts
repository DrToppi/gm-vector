import { useEffect, useRef, useState } from "react";
import { tr } from "./i18n";
import { usePlanningClock } from "./PlanningClock";
import { useSessionScope } from "./useSessionScope";
import { WorkspaceRefresh, type WorkspaceIntent } from "./workspaceRefresh";
import { download, onSessionChange } from "./api";
import { reportCSV, reportHTML } from "./exportReport";
import { projectPayload } from "./projectPayload";
import type { PortableProject } from "./projectTransfer";
import {
  blankTask,
  clone,
  type Calendar,
  type Employee,
  type Link,
  type Project,
  type Task,
  type User,
} from "./types";
export function useWorkspaceController({
  user,
  onLogout,
}: {
  user: User;
  onLogout: () => void;
}) {
  const clock = usePlanningClock();
  const scope = useSessionScope();
  const api = scope.api;
  const [projects, setProjects] = useState<
    { id: string; title: string; task_count: number }[]
  >([]);
  const [project, setProject] = useState<Project | null>(null);
  const [role, setRole] = useState("viewer");
  const [employees, setEmployees] = useState<Employee[]>([]);
  const [calendars, setCalendars] = useState<Calendar[]>([]);
  const [view, setView] = useState("gantt");
  const [selected, setSelected] = useState("");
  const [trace, setTrace] = useState("");
  const [taskEditor, setTaskEditor] = useState<Task | null>(null);
  const [linkEditor, setLinkEditor] = useState<{
    link: Link;
    index: number;
  } | null>(null);
  const [settings, setSettings] = useState(false);
  const [members, setMembers] = useState(false);
  const [creating, setCreating] = useState(false);
  const [preview, setPreview] = useState<Project | null>(null);
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const canManage = role === "manager";
  const canWrite = role !== "viewer";
  const refresh = useRef(new WorkspaceRefresh(scope.current)).current;
  const intent = refresh.capture();
  const readError = useRef<{
    family: "project" | "lists";
    intent: WorkspaceIntent;
  } | null>(null);
  const currentUI = useRef({ project, paused: false });
  currentUI.current = {
    project,
    paused: !!(
      preview ||
      taskEditor ||
      linkEditor ||
      settings ||
      members ||
      creating ||
      busy ||
      view === "admin"
    ),
  };
  const fail = (
    e: any,
    ticket: WorkspaceIntent = intent,
    family?: "project" | "lists",
  ) => {
    if (scope.current() && ticket.current() && e.name !== "AbortError") {
      readError.current = family ? { family, intent: refresh.capture() } : null;
      setError(e.message || String(e));
    }
  };
  const clearReadError = (family: "project" | "lists") => {
    if (
      readError.current?.family === family &&
      readError.current.intent.current()
    ) {
      readError.current = null;
      setError("");
    }
  };
  async function refreshLists(expected: WorkspaceIntent = intent) {
    if (!expected.current()) return;
    const ticket = refresh.read("lists");
    try {
      const [p, e, c] = await Promise.all([
        api("/projects", "GET", undefined, ticket.signal),
        api("/employees", "GET", undefined, ticket.signal),
        api("/calendars", "GET", undefined, ticket.signal),
      ]);
      if (ticket.current()) {
        setProjects(p);
        setEmployees(e);
        setCalendars(c);
        clearReadError("lists");
      }
    } catch (e) {
      fail(e, ticket, "lists");
    } finally {
      ticket.finish();
      // A legitimate newer catalog can supersede an opening project batch. Once
      // that catalog settles, retry the selected read so its workspace cannot stay
      // empty. Old child callbacks are rejected at entry and cannot queue this.
      if (ticket.current() && !currentUI.current.project && refresh.projectID)
        void loadProject(refresh.projectID, false, expected);
    }
  }
  async function loadProject(
    id: string,
    background = false,
    expected: WorkspaceIntent = intent,
  ) {
    if (!expected.current() || id !== refresh.projectID || !scope.current())
      return;
    const ticket = refresh.read("project", background);
    try {
      const [r, e] = await Promise.all([
        api("/projects/" + id, "GET", undefined, ticket.signal),
        api("/employees", "GET", undefined, ticket.signal),
      ]);
      if (
        background &&
        (currentUI.current.paused || document.querySelector("dialog[open]"))
      )
        return;
      if (refresh.accept(ticket, r.project)) {
        setProject(r.project);
        setRole(r.role);
        setEmployees(e);
        clearReadError("project");
        if (!background) refresh.markFresh();
      }
    } catch (e) {
      if (!background) fail(e, ticket, "project");
    } finally {
      ticket.finish();
    }
  }
  async function open(id: string) {
    const selectedIntent = refresh.select(id);
    setProject(null);
    setRole("viewer");
    setSelected("");
    setTrace("");
    setTaskEditor(null);
    setLinkEditor(null);
    setSettings(false);
    setMembers(false);
    setCreating(false);
    setPreview(null);
    setBusy(false);
    setError("");
    await loadProject(id, false, selectedIntent);
  }
  // The polling subscription retains the original session-lifetime handlers;
  // child callbacks continue to capture their own render-bound intent.
  const initialPolling = useRef({ loadProject, refreshLists });
  const latestOpen = useRef(open);
  latestOpen.current = open;
  useEffect(() => {
    const initial = initialPolling.current;
    refresh.connectPolling(
      () => initial.loadProject(refresh.projectID, true, refresh.capture()),
      () => !currentUI.current.paused,
    );
    void initial.refreshLists();
    return () => refresh.close();
  }, [refresh]);
  useEffect(() => {
    if (user && !project && !refresh.projectID && projects.length)
      void latestOpen.current(projects[0].id);
  }, [user, project, projects, refresh]);
  useEffect(
    () => refresh.updatePolling(),
    [
      refresh,
      project,
      preview,
      taskEditor,
      linkEditor,
      settings,
      members,
      creating,
      busy,
      view,
    ],
  );
  async function propose(p: Project): Promise<boolean> {
    if (!canWrite || !intent.current()) return false;
    const operation = refresh.beginOperation(p.id);
    if (!operation) return false;
    setBusy(true);
    setError("");
    try {
      const next = await api("/projects/" + p.id + "/preview", "POST", {
        project: p,
        override_reason: "",
      });
      if (!refresh.accept(operation, next)) return false;
      setTaskEditor(null);
      setLinkEditor(null);
      setSettings(false);
      setPreview(next);
      setReason("");
      return true;
    } catch (e) {
      fail(e, operation);
      return false;
    } finally {
      if (operation.current()) setBusy(false);
      operation.finish();
    }
  }
  async function saveGesture(p: Project) {
    if (!canWrite || !intent.current()) return;
    const operation = refresh.beginOperation(p.id);
    if (!operation) return;
    setBusy(true);
    setError("");
    try {
      const saved = await api<Project>("/projects/" + p.id, "PUT", {
        project: p,
        override_reason: "",
      });
      if (refresh.accept(operation, saved)) {
        setProject(saved);
        refresh.markFresh();
      }
    } catch (e) {
      fail(e, operation);
    } finally {
      if (operation.current()) setBusy(false);
      operation.finish();
    }
  }
  async function commit() {
    if (!preview || !intent.current()) return;
    const operation = refresh.beginOperation(preview.id);
    if (!operation) return;
    setBusy(true);
    try {
      const p = await api("/projects/" + preview.id, "PUT", {
        project: preview,
        override_reason: reason,
      });
      if (!refresh.accept(operation, p)) return;
      setProject(p);
      setPreview(null);
      refresh.markFresh();
      await refreshLists();
    } catch (e) {
      fail(e, operation);
    } finally {
      if (operation.current()) setBusy(false);
      operation.finish();
    }
  }
  async function undo() {
    if (!project || !canWrite || !intent.current()) return;
    const operation = refresh.beginOperation(project.id);
    if (!operation) return;
    setBusy(true);
    setError("");
    try {
      const p = await api<Project>(
        "/projects/" + project.id + "/undo",
        "POST",
        { revision: project.revision },
      );
      if (refresh.accept(operation, p)) {
        setProject(p);
        refresh.markFresh();
        await refreshLists();
      }
    } catch (e) {
      fail(e, operation);
    } finally {
      if (operation.current()) setBusy(false);
      operation.finish();
    }
  }
  async function archiveProject() {
    if (!project || !canManage || !intent.current()) return;
    const operation = refresh.beginOperation(project.id);
    if (!operation) return;
    setBusy(true);
    try {
      await api("/projects/" + project.id, "DELETE", {
        revision: project.revision,
      });
      if (!operation.current()) return;
      setProjects((current) => current.filter((p) => p.id !== project.id));
      setProject(null);
      setSettings(false);
      setError("");
      setBusy(false);
      const archivedIntent = refresh.select("");
      await refreshLists(archivedIntent);
    } catch (e) {
      fail(e, operation);
      throw e;
    } finally {
      if (operation.current()) setBusy(false);
      operation.finish();
    }
  }
  function addChild(parent: Task) {
    if (!project || !canManage || !canWrite) return;
    const child = blankTask(parent.id, parent.start || project.start);
    child.responsibility = parent.responsibility;
    refresh.pause();
    setError("");
    setTaskEditor(child);
  }
  function saveTask(t: Task) {
    if (!project) return;
    const p = clone(project);
    const i = p.tasks.findIndex((x) => x.id === t.id);
    if (i >= 0) p.tasks[i] = t;
    else p.tasks.push(t);
    propose(p);
  }
  function deleteTask() {
    if (!project || !taskEditor) return;
    const doomed = new Set([taskEditor.id]);
    let changed = true;
    while (changed) {
      changed = false;
      for (const t of project.tasks)
        if (doomed.has(t.parent) && !doomed.has(t.id)) {
          doomed.add(t.id);
          changed = true;
        }
    }
    const p = clone(project);
    p.tasks = p.tasks.filter((t) => !doomed.has(t.id));
    p.links = p.links.filter((l) => !doomed.has(l.from) && !doomed.has(l.to));
    propose(p);
  }
  async function exportProject(
    format: "html" | "json" | "pdf" | "csv" | "portable",
  ) {
    if (!project || !scope.current() || !intent.current()) return;
    const printWindow = format === "pdf" ? window.open("", "_blank") : null;
    if (format === "pdf" && !printWindow) {
      setError(tr("m2891165d5a"));
      return;
    }
    const unsubscribe = onSessionChange(() => printWindow?.close());
    try {
      if (format === "portable") {
        const document = await api<PortableProject>(
          "/projects/" + project.id + "/portable/export",
        );
        scope.assert();
        if (intent.current())
          download(
            "gm-vector-project-portable.json",
            JSON.stringify(document, null, 2),
            "application/json",
          );
        return;
      }
      const p = await api<Project>("/projects/" + project.id + "/export");
      scope.assert();
      if (!intent.current()) {
        printWindow?.close();
        return;
      }
      if (format === "json")
        download(
          "gma-project.json",
          JSON.stringify(projectPayload(p), null, 2),
          "application/json",
        );
      else if (format === "csv")
        download(
          "gma-project.csv",
          reportCSV(p, employees, clock.today()),
          "text/csv;charset=utf-8",
        );
      else {
        const html = reportHTML(p, employees, clock.today());
        if (printWindow) {
          printWindow.opener = null;
          printWindow.document.open();
          printWindow.document.write(html);
          printWindow.document.close();
          printWindow.focus();
          window.setTimeout(() => {
            if (scope.current() && intent.current() && !printWindow.closed)
              printWindow.print();
            else printWindow.close();
          }, 150);
        } else download("gma-project.html", html, "text/html;charset=utf-8");
      }
    } catch (e) {
      printWindow?.close();
      unsubscribe();
      fail(e);
    } finally {
      if (!printWindow) unsubscribe();
    }
  }
  return {
    addChild,
    archiveProject,
    busy,
    calendars,
    canManage,
    canWrite,
    clock,
    commit,
    creating,
    deleteTask,
    employees,
    error,
    exportProject,
    filter,
    intent,
    linkEditor,
    loadProject,
    members,
    open,
    preview,
    project,
    projects,
    propose,
    reason,
    refresh,
    refreshLists,
    role,
    saveGesture,
    saveTask,
    selected,
    setCreating,
    setError,
    setFilter,
    setLinkEditor,
    setMembers,
    setPreview,
    setReason,
    setSelected,
    setSettings,
    setTaskEditor,
    setTrace,
    setView,
    settings,
    taskEditor,
    trace,
    undo,
    view,
  };
}
