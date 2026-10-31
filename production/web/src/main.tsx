import { ProjectImport } from "./ProjectImport";
import { HistoryExportActions } from "./HistoryExportActions";
import { historyFilters } from "./history";
import type { PortableProject } from "./projectTransfer";
import { PlanningClockProvider, usePlanningClock } from "./PlanningClock";
import { projectPayload } from "./projectPayload";
import {
  calendarForProject,
  MIN_PLANNING_DATE,
  MAX_PLANNING_DATE,
} from "./planningCalendar";
import { ProvisionalCalendarNotice } from "./ProjectCalendars";
import { tr, changeLocale, isLocale } from "./i18n";
import { useTranslation } from "react-i18next";
import { LanguagePicker } from "./LanguagePicker";
import { useSessionScope } from "./useSessionScope";
import { WorkspaceRefresh, type WorkspaceIntent } from "./workspaceRefresh";
import { BugReport } from "./BugReport";
import { installDiagnostics, setDiagnosticIdentity } from "./diagnostics";
import { reportCSV, reportHTML } from "./exportReport";
import { taskStatus, taskExecutors, scheduleAvailable } from "./taskStatus";
import { initials } from "./initials";
import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  api as rootAPI,
  download,
  setCSRF,
  advanceSession,
  isCurrentSession,
  sessionEpoch,
  onSessionChange,
} from "./api";
import { Auth } from "./Auth";
import { Chart } from "./Chart";
import {
  LinkEditor,
  Modal,
  PeoplePanel,
  ProjectEditor,
  TaskEditor,
} from "./Editors";
import { AdminPanel, MembersPanel } from "./Panels";
import {
  blankTask,
  clone,
  dateNum,
  hours,
  statuses,
  today,
  type Calendar,
  type Employee,
  type Link,
  type Project,
  type Task,
  type User,
} from "./types";
import "./style.css";
installDiagnostics();
function App() {
  useTranslation();
  const [state, setState] = useState<{
    phase: "loading" | "login" | "ready";
    epoch: number;
    user?: User;
    setup?: boolean;
    error?: string;
    planningZone?: string;
  }>({ phase: "loading", epoch: sessionEpoch() });
  async function auth(expected = sessionEpoch()) {
    if (!isCurrentSession(expected)) return;
    const epoch = advanceSession();
    setState({ phase: "loading", epoch });
    try {
      const m = await rootAPI("/auth/me");
      if (!isCurrentSession(epoch)) return;
      setCSRF(m.csrf);
      setDiagnosticIdentity(m.user.id);
      await changeLocale(isLocale(m.user.locale) ? m.user.locale : "ru");
      if (!isCurrentSession(epoch)) return;
      setState({
        phase: "ready",
        epoch,
        user: m.user,
        planningZone:
          typeof m.planning_timezone === "string" ? m.planning_timezone : "",
      });
    } catch {
      if (!isCurrentSession(epoch)) return;
      try {
        const s = await rootAPI("/auth/status");
        if (isCurrentSession(epoch))
          setState({ phase: "login", epoch, setup: s.setup_required });
      } catch {
        if (isCurrentSession(epoch))
          setState({ phase: "login", epoch, error: tr("md380c5043d") });
      }
    }
  }
  async function logout(expected: number) {
    if (!isCurrentSession(expected)) return;
    const epoch = advanceSession(true);
    setState({ phase: "loading", epoch });
    const pending = rootAPI("/auth/logout", "POST", {});
    setCSRF("");
    try {
      await pending;
      if (isCurrentSession(epoch))
        setState({ phase: "login", epoch, setup: false });
    } catch {
      if (isCurrentSession(epoch))
        setState({
          phase: "login",
          epoch,
          setup: false,
          error: tr("me984796ab8"),
        });
    }
  }
  useEffect(() => {
    void auth();
  }, []);
  if (state.phase === "loading")
    return (
      <>
        <LanguagePicker />
        <div className="loading">{tr("mf35bba9987")}</div>
        <BugReport key={state.epoch} view="loading" />
      </>
    );
  if (state.phase === "login")
    return (
      <React.Fragment key={state.epoch}>
        <LanguagePicker />
        {state.error && (
          <p className="error" role="alert">
            {state.error}
          </p>
        )}
        <Auth setup={!!state.setup} onReady={() => void auth(state.epoch)} />
        <BugReport view="login" />
      </React.Fragment>
    );
  return (
    <PlanningClockProvider zone={state.planningZone!}>
      <LanguagePicker authenticated />
      <Workspace
        key={state.epoch}
        user={state.user!}
        onLogout={() => void logout(state.epoch)}
      />
    </PlanningClockProvider>
  );
}
function Workspace({ user, onLogout }: { user: User; onLogout: () => void }) {
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
  useEffect(() => {
    refresh.connectPolling(
      () => loadProject(refresh.projectID, true, refresh.capture()),
      () => !currentUI.current.paused,
    );
    void refreshLists();
    return () => refresh.close();
  }, []);
  useEffect(() => {
    if (user && !project && !refresh.projectID && projects.length)
      void open(projects[0].id);
  }, [user, project, projects]);
  useEffect(
    () => refresh.updatePolling(),
    [
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

  const leaves = project?.tasks.filter((t) => !t.summary) || [];
  const done = leaves.filter((t) => t.status === "done").length;
  const calendar = project ? calendarForProject(project, calendars) : undefined;
  return (
    <div className="app">
      <aside>
        <div className="brand">
          gm<span>Vector</span>
        </div>
        <p className="sidebar-label">{tr("mbb9ec32d29")}</p>
        <nav>
          {projects.map((p) => (
            <button
              key={p.id}
              className={
                project?.id === p.id && view !== "admin" ? "active" : ""
              }
              onClick={() => {
                open(p.id);
                setView("gantt");
              }}
            >
              <span>◇</span>
              <span>{p.title}</span>
              <small>{p.task_count}</small>
            </button>
          ))}
        </nav>
        {user.admin && (
          <button
            className="sidebar-new"
            onClick={() => {
              refresh.pause();
              setCreating(true);
            }}
          >
            {tr("m3bb1aac684")}
          </button>
        )}
        <div className="sidebar-bottom">
          {user.admin && (
            <button
              className={view === "admin" ? "active" : ""}
              onClick={() => setView("admin")}
            >
              {tr("m5f6b847204")}
            </button>
          )}
          <div className="identity">
            <div className="avatar">{initials(user.name)}</div>
            <span>
              {user.name}
              <small>
                {user.admin ? tr("m6c7719973e") : tr("m6998addb06")}
              </small>
            </span>
          </div>
          <button onClick={onLogout}>{tr("m5690c2e63b")}</button>
        </div>
      </aside>
      <main className="workspace">
        <div className="topbar">
          <span>
            {tr("m7c32a50a84")}{" "}
            {view === "admin" ? tr("md1ed190459") : tr("m9c47d4aac0")}
          </span>
          <span className="badge">
            <i /> {tr("m24ad018825")}
          </span>
        </div>
        {error && !taskEditor && !linkEditor && !preview && !settings && (
          <div className="error global-error" role="alert">
            {error}
            <button className="icon" onClick={() => setError("")}>
              ×
            </button>
          </div>
        )}
        {view === "admin" ? (
          <AdminPanel />
        ) : project ? (
          <>
            <header
              className="project-header"
              data-calculation-state={project.calculation_state || "ready"}
            >
              <div>
                <p className="eyebrow">
                  {tr("m9bf989e674")}{" "}
                  {project.external_reference || tr("mdfa820300f")}
                </p>
                <h1>{project.title}</h1>
                <p>{project.brief || tr("m238ab07cc9")}</p>
              </div>
              {canManage && (
                <button
                  className="secondary"
                  onClick={() => {
                    refresh.pause();
                    setSettings(true);
                  }}
                >
                  {tr("md367c360b8")}
                </button>
              )}
            </header>
            <div className="metrics">
              <article>
                <span>{tr("m03cc1c2d5a")}</span>
                <b>{leaves.length}</b>
                <small>
                  {done} {tr("mf40eba382c")}
                </small>
              </article>
              <article>
                <span>{tr("mf5ebb4335b")}</span>
                <b>
                  {leaves.length ? Math.round((done / leaves.length) * 100) : 0}
                  <em>%</em>
                </b>
                <div className="progress">
                  <i
                    style={{
                      width:
                        (leaves.length ? (done / leaves.length) * 100 : 0) +
                        "%",
                    }}
                  />
                </div>
              </article>
              <article>
                <span>{tr("m0742c60990")}</span>
                <b>
                  {hours(leaves.reduce((s, t) => s + t.planned_minutes, 0))}
                  <em>
                    {" "}
                    / {hours(
                      leaves.reduce((s, t) => s + t.actual_minutes, 0),
                    )}{" "}
                    {tr("m40831c5d98")}
                  </em>
                </b>
                <small>{tr("m463ff0b6ba")}</small>
              </article>
              <article>
                <span>{tr("m8b372c12c2")}</span>
                <b>{project.people.length}</b>
                <small>
                  {project.duration_mode === "working"
                    ? tr("ma187641833")
                    : tr("m23b1951497")}{" "}
                  {tr("m5dfe892a20")}
                </small>
              </article>
            </div>
            <div className="viewbar">
              <div className="tabs">
                {[
                  ["gantt", tr("m0aea1e159e")],
                  ["map", tr("ma2b17cfd8b")],
                  ["list", tr("m03cc1c2d5a")],
                  ["people", tr("m19a8fb318b")],
                ].map(([v, l]) => (
                  <button
                    key={v}
                    className={view === v ? "active" : ""}
                    onClick={() => {
                      setView(v);
                      setTrace("");
                    }}
                  >
                    {l}
                  </button>
                ))}
              </div>
              <div className="actions">
                {canManage && (
                  <details className="project-actions export-menu">
                    <summary>{tr("m7438bfa3ce")}</summary>
                    <div>
                      {(
                        ["html", "json", "portable", "pdf", "csv"] as const
                      ).map((format) => (
                        <button
                          key={format}
                          data-export-format={format}
                          className="secondary"
                          onClick={() => exportProject(format)}
                        >
                          {format === "portable"
                            ? tr("portable.export")
                            : format === "pdf"
                              ? tr("m804901710c")
                              : format.toUpperCase()}
                        </button>
                      ))}
                    </div>
                  </details>
                )}
                {canManage && (
                  <HistoryExportActions
                    projectID={project.id}
                    filters={historyFilters()}
                    revision={project.revision}
                  />
                )}
                <details className="project-actions">
                  <summary>{tr("mcad123465b")}</summary>
                  <div>
                    {canManage && (
                      <>
                        <button
                          className="secondary"
                          onClick={() => {
                            refresh.pause();
                            setMembers(true);
                          }}
                        >
                          {tr("ma6a60f97b0")}
                        </button>
                      </>
                    )}
                    {canWrite && (
                      <>
                        <button
                          className="secondary"
                          disabled={busy}
                          onClick={undo}
                        >
                          ↶
                        </button>
                        <button
                          disabled={!canManage}
                          onClick={() => {
                            refresh.pause();
                            setTaskEditor(blankTask("", project.start));
                          }}
                        >
                          {tr("m4d4e55ccf1")}
                        </button>
                      </>
                    )}
                  </div>
                </details>
              </div>
            </div>
            {["gantt", "map", "list", "effort"].includes(view) && (
              <div className="project-search">
                <input
                  aria-label={tr("m1825ac465d")}
                  placeholder={tr("mda2369b560")}
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                />
              </div>
            )}
            {project.calculation_state &&
              project.calculation_state !== "ready" && (
                <div className="calculation-state notice amber" role="status">
                  <strong>
                    {project.calculation_state === "needs_participation"
                      ? tr("m0d7a57df69")
                      : tr("m4758de3893")}
                  </strong>
                  <span>
                    {project.calculation_state === "needs_participation"
                      ? tr("mdbc2b187b0")
                      : tr("meaf2cc4bc5")}
                  </span>
                </div>
              )}
            <ProvisionalCalendarNotice project={project} />
            {(project.warnings?.length || 0) > 0 && (
              <details className="warnings">
                <summary>
                  {tr("mac67d983b2")} {project.warnings?.length}
                </summary>
                <ul>
                  {project.warnings?.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ul>
              </details>
            )}
            {view === "gantt" || view === "map" ? (
              <Chart
                key={project.id}
                filter={filter}
                project={project}
                calendar={calendar}
                view={view}
                selected={selected}
                setSelected={setSelected}
                onChild={addChild}
                onEdit={(t) => {
                  refresh.pause();
                  setTaskEditor(t);
                }}
                onLink={(link, index) => {
                  if (canManage && canWrite) {
                    refresh.pause();
                    setLinkEditor({ link, index });
                  }
                }}
                onChange={saveGesture}
                trace={trace}
                setTrace={(id) => {
                  setTrace(id);
                  if (id) setView("map");
                }}
                canEdit={canManage && canWrite && !busy}
              />
            ) : view === "people" ? (
              <PeoplePanel
                project={project}
                employees={employees}
                admin={user.admin}
                canEdit={!!(canManage && canWrite)}
                requestError={error}
                onChange={propose}
                onRefresh={refreshLists}
              />
            ) : (
              <section className="panel">
                <div className="toolbar">
                  {canManage && project.tasks.length > 1 && (
                    <button
                      className="secondary"
                      onClick={() => {
                        refresh.pause();
                        setLinkEditor({
                          link: {
                            from: project.tasks[0].id,
                            to: project.tasks[1].id,
                            mode: "after",
                            days: 0,
                          },
                          index: -1,
                        });
                      }}
                    >
                      {tr("mb86d20bc55")}
                    </button>
                  )}
                </div>
                <table>
                  <thead>
                    <tr>
                      <th>{tr("mb9ce27191d")}</th>
                      <th>{tr("m225077c6d4")}</th>
                      <th>{tr("mb200273ada")}</th>
                      <th>{tr("m86265f05c0")}</th>
                      <th>{tr("m037174d588")}</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {project.tasks
                      .filter((t) =>
                        t.title.toLowerCase().includes(filter.toLowerCase()),
                      )
                      .map((t) => (
                        <tr
                          key={t.id}
                          data-milestone={t.summary ? t.id : undefined}
                          className={
                            (t.summary ? "milestone-row " : "") +
                            (t.status === "done"
                              ? "done"
                              : t.blocked
                                ? "blocked"
                                : t.responsibility === "customer"
                                  ? "customer"
                                  : "")
                          }
                        >
                          <td>
                            {t.summary && (
                              <span
                                className="milestone-badge"
                                data-milestone-label={t.id}
                              >
                                {tr("mcde20c380c")}{" "}
                                {
                                  project.tasks.filter((x) => x.parent === t.id)
                                    .length
                                }{" "}
                                {tr("m1d8f1eeefb")}
                              </span>
                            )}
                            <button
                              className="text-button title"
                              onClick={() => {
                                refresh.pause();
                                setTaskEditor(t);
                              }}
                            >
                              {t.title}
                            </button>
                            <small>{t.brief}</small>
                          </td>
                          <td>
                            <span
                              role="img"
                              className={
                                "task-status-dot " +
                                taskStatus(t, project, clock.today()).kind
                              }
                              data-task-status={
                                taskStatus(t, project, clock.today()).kind
                              }
                              title={
                                taskStatus(t, project, clock.today()).label
                              }
                              aria-label={
                                taskStatus(t, project, clock.today()).label
                              }
                            />
                          </td>
                          <td>
                            {scheduleAvailable(t, project) ? (
                              <>
                                {t.start || tr("m65fd24f1cb")}
                                {t.end && " — " + t.end}
                              </>
                            ) : (
                              tr("m7147f1233e")
                            )}
                          </td>
                          <td
                            className={
                              t.actual_minutes > t.planned_minutes
                                ? "effort-overrun"
                                : undefined
                            }
                          >
                            {hours(t.planned_minutes)} /{" "}
                            {hours(t.actual_minutes)}
                          </td>
                          <td>{taskExecutors(t, employees)}</td>
                          <td>
                            {canManage && canWrite && (
                              <button
                                className="secondary"
                                aria-label={tr("m39dcb19084") + t.title}
                                onClick={() => addChild(t)}
                              >
                                ＋
                              </button>
                            )}
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </section>
            )}
          </>
        ) : (
          <div className="empty">
            <h1>{tr("mc5e22cfaa3")}</h1>
            <p>{tr("mca6e44952a")}</p>
            {user.admin && (
              <button
                onClick={() => {
                  refresh.pause();
                  setCreating(true);
                }}
              >
                {tr("m49ed84ac64")}
              </button>
            )}
          </div>
        )}
      </main>
      {project && taskEditor && (
        <TaskEditor
          error={error}
          key={taskEditor.id}
          task={taskEditor}
          project={project}
          employees={employees}
          canManage={!!(canManage && canWrite)}
          canWrite={!!canWrite}
          user={user}
          onRefresh={() => loadProject(project.id)}
          onSave={saveTask}
          onDelete={deleteTask}
          onChild={() => addChild(taskEditor)}
          onClose={() => {
            setTaskEditor(null);
            setError("");
          }}
        />
      )}{" "}
      {project && linkEditor && (
        <LinkEditor
          error={error}
          link={linkEditor.link}
          project={project}
          onSave={(l) => {
            const p = clone(project);
            if (linkEditor.index >= 0) p.links[linkEditor.index] = l;
            else p.links.push(l);
            propose(p);
          }}
          onDelete={() => {
            const p = clone(project);
            if (linkEditor.index >= 0) p.links.splice(linkEditor.index, 1);
            propose(p);
          }}
          onClose={() => {
            setLinkEditor(null);
            setError("");
          }}
        />
      )}{" "}
      {settings && project && (
        <ProjectEditor
          error={error}
          project={project}
          calendars={calendars}
          onSave={propose}
          onDelete={archiveProject}
          onClose={() => {
            setSettings(false);
            setError("");
          }}
        />
      )}{" "}
      {members && project && (
        <MembersPanel project={project} onClose={() => setMembers(false)} />
      )}{" "}
      {preview && (
        <Modal title={tr("mb0041fe655")} onClose={() => setPreview(null)}>
          <p
            className={
              preview.calculation_state && preview.calculation_state !== "ready"
                ? "notice amber"
                : undefined
            }
            data-preview-calculation-state={
              preview.calculation_state || "ready"
            }
          >
            {preview.calculation_state === "needs_participation"
              ? tr("m724d24de93")
              : preview.calculation_state === "blocked_capacity"
                ? tr("md5e10c0399")
                : tr("m304aef502f")}
          </p>
          <ProvisionalCalendarNotice project={preview} />
          <div className="preview-list">
            {preview.tasks
              .filter((t) => {
                const old = project?.tasks.find((x) => x.id === t.id);
                return !old || old.start !== t.start || old.end !== t.end;
              })
              .map((t) => (
                <div key={t.id}>
                  <b>{t.title}</b>
                  <span>
                    {t.start || tr("m65fd24f1cb")} —{" "}
                    {t.end || tr("ma8efd0c322")}
                  </span>
                </div>
              ))}
          </div>
          {preview.warnings?.length ? (
            <details>
              <summary>
                {tr("m6ae9b8896d")} {preview.warnings.length}
              </summary>
              <ul>
                {preview.warnings.map((s, i) => (
                  <li key={i}>{s}</li>
                ))}
              </ul>
            </details>
          ) : (
            <p>{tr("m48531e348f")}</p>
          )}
          {canManage &&
            preview.tasks.some(
              (t) =>
                !t.summary &&
                t.blocked &&
                t.status !== "todo" &&
                t.status !== project?.tasks.find((x) => x.id === t.id)?.status,
            ) && (
              <label>
                {tr("mfed5c93b71")}
                <input
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  maxLength={2000}
                />
              </label>
            )}
          {error && <p className="error">{error}</p>}
          <footer>
            <button className="secondary" onClick={() => setPreview(null)}>
              {tr("m8fbe9b75cb")}
            </button>
            <button disabled={busy} onClick={commit}>
              {busy ? tr("m81742d9d13") : tr("m7dd00b6b08")}
            </button>
          </footer>
        </Modal>
      )}{" "}
      {creating && (
        <CreateProject
          calendars={calendars}
          onClose={() => setCreating(false)}
          onCreated={async (id) => {
            if (!intent.current()) return;
            setCreating(false);
            await refreshLists();
            if (intent.current()) {
              void open(id);
              setView("gantt");
            }
          }}
        />
      )}
      <BugReport key={user.id} authenticated admin={user.admin} view={view} />
    </div>
  );
}
function CreateProject({
  calendars,
  onClose,
  onCreated,
}: {
  calendars: Calendar[];
  onClose: () => void;
  onCreated: (id: string) => void | Promise<void>;
}) {
  const clock = usePlanningClock(),
    { api } = useSessionScope();
  const [title, setTitle] = useState(""),
    [start, setStart] = useState(() => clock.today()),
    [calendar, setCalendar] = useState(""),
    [error, setError] = useState("");
  const selectedCalendar = calendars.some((c) => c.id === calendar)
    ? calendar
    : calendars[0]?.id || "";
  return (
    <Modal title={tr("m15d568b010")} onClose={onClose}>
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          try {
            const p = await api("/projects", "POST", {
              title,
              start,
              calendar_id: selectedCalendar,
              duration_mode: "working",
            });
            await onCreated(p.id);
          } catch (cause) {
            setError((cause as Error).message);
          }
        }}
      >
        <label>
          {tr("m0918b4ba92")}
          <input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            required
            maxLength={200}
          />
        </label>
        <label>
          {tr("m4af5d2efad")}
          <input
            type="date"
            min={MIN_PLANNING_DATE}
            max={MAX_PLANNING_DATE}
            value={start}
            onChange={(event) => setStart(event.target.value)}
            required
          />
        </label>
        <label>
          {tr("m2ea15d96b2")}
          <select
            disabled={!calendars.length}
            value={selectedCalendar}
            onChange={(event) => setCalendar(event.target.value)}
          >
            {calendars.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
        <p className="muted">
          {tr("clock.zone")}: {clock.zone}
        </p>
        {error && <p className="error">{error}</p>}
        <footer>
          <button disabled={!selectedCalendar || !clock.ready}>
            {tr("me2be34974a")}
          </button>
        </footer>
      </form>
      <ProjectImport onCreated={onCreated} />
    </Modal>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
