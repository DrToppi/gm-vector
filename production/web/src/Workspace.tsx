import { tr } from "./i18n";
import { useWorkspaceController } from "./useWorkspaceController";
import { calendarForProject } from "./planningCalendar";
import { ProvisionalCalendarNotice } from "./ProjectCalendars";
import { taskStatus, taskExecutors, scheduleAvailable } from "./taskStatus";
import { initials } from "./initials";
import { blankTask, clone, hours, type User } from "./types";
import { Chart } from "./Chart";
import { LinkEditor, PeoplePanel, ProjectEditor, TaskEditor } from "./Editors";
import { Modal } from "./Modal";
import { AdminPanel, MembersPanel } from "./Panels";
import { CreateProject } from "./CreateProject";
import { BugReport } from "./BugReport";
import { HistoryExportActions } from "./HistoryExportActions";
import { historyFilters } from "./history";
export function Workspace({
  user,
  onLogout,
}: {
  user: User;
  onLogout: () => void;
}) {
  const {
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
  } = useWorkspaceController({ user, onLogout });

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
