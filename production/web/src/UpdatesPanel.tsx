import { useCallback, useEffect, useRef, useState } from "react";
import { tr } from "./i18n";
import { useSessionScope } from "./useSessionScope";
import {
  UpdateRequests,
  updateStatus,
  updateRunning,
  updateCanApply,
  updateCanRecover,
  type UpdateStatus,
} from "./updaterState";
import "./updates.css";

const pollDelay = 2000;
const maxAutomaticPolls = 450;
const phaseKeys: Record<string, string> = {
  queued: "updates.phase.queued",
  fetch: "updates.phase.fetch",
  build: "updates.phase.build",
  preflight: "updates.phase.preflight",
  backup: "updates.phase.backup",
  migrate: "updates.phase.migrate",
  start: "updates.phase.start",
  verify: "updates.phase.verify",
  restore: "updates.phase.restore",
  complete: "updates.phase.complete",
};

export function UpdatesPanel() {
  const scope = useSessionScope();
  const requests = useRef(new UpdateRequests()).current;
  const mounted = useRef(false);
  const mutating = useRef(false);
  const automaticPolls = useRef(0);
  const previousStatus = useRef<UpdateStatus | null>(null);
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [ref, setRef] = useState("main");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState("");
  const [paused, setPaused] = useState(false);
  const [revision, setRevision] = useState(0);
  const current = useCallback(
    () => mounted.current && scope.current(),
    [scope],
  );
  const resetConfirmation = useCallback((next: UpdateStatus) => {
    const previous = previousStatus.current;
    if (
      previous?.candidate_commit !== next.candidate_commit ||
      previous?.operation_id !== next.operation_id ||
      previous?.state !== next.state
    )
      setConfirmation("");
    previousStatus.current = next;
    setStatus(next);
  }, []);
  const refresh = useCallback(
    async (manual = false) => {
      if (!current() || mutating.current) return;
      if (manual) {
        automaticPolls.current = 0;
        setPaused(false);
      }
      const ticket = requests.begin();
      try {
        const next = updateStatus(
          await scope.api("/admin/updates", "GET", undefined, ticket.signal),
        );
        if (!current() || !ticket.current()) return;
        resetConfirmation(next);
        setUncertain(false);
        setError("");
      } catch {
        if (!current() || !ticket.current()) return;
        setError(tr("updates.unavailable"));
      } finally {
        if (current() && ticket.current()) setRevision((value) => value + 1);
      }
    },
    [current, requests, scope, resetConfirmation],
  );
  useEffect(() => {
    mounted.current = true;
    requests.open();
    void refresh();
    return () => {
      mounted.current = false;
      requests.close();
    };
  }, [requests, refresh]);
  const running = !!status && updateRunning(status.state);
  useEffect(() => {
    if (busy || paused || (!running && !uncertain)) return;
    if (automaticPolls.current >= maxAutomaticPolls) {
      setPaused(true);
      return;
    }
    const timer = window.setTimeout(() => {
      automaticPolls.current++;
      void refresh();
    }, pollDelay);
    return () => window.clearTimeout(timer);
  }, [running, uncertain, busy, paused, revision, refresh]);
  async function command(action: "check" | "apply" | "recover") {
    if (
      !current() ||
      mutating.current ||
      !status?.enabled ||
      running ||
      uncertain
    )
      return;
    if (action === "apply" && !updateCanApply(status, confirmation)) return;
    if (action === "recover" && !updateCanRecover(status, confirmation)) return;
    const selectedRef = ref.trim();
    if (action === "check" && !selectedRef) return;
    mutating.current = true;
    setBusy(true);
    setError("");
    setPaused(false);
    automaticPolls.current = 0;
    const ticket = requests.begin();
    const body =
      action === "check"
        ? { ref: selectedRef }
        : action === "apply"
          ? { commit: status.candidate_commit, confirmation }
          : { operation_id: status.operation_id, confirmation };
    try {
      const next = updateStatus(
        await scope.api(
          "/admin/updates/" + action,
          "POST",
          body,
          ticket.signal,
        ),
      );
      if (!current() || !ticket.current()) return;
      resetConfirmation(next);
      setConfirmation("");
      setUncertain(false);
    } catch {
      if (!current() || !ticket.current()) return;
      // The host may already have accepted the command. Reconcile status; never
      // retry a mutating request automatically or encourage a duplicate apply.
      setUncertain(true);
      setConfirmation("");
      setError(tr("updates.uncertain"));
    } finally {
      if (current() && ticket.current()) {
        mutating.current = false;
        setBusy(false);
        setRevision((value) => value + 1);
      }
    }
  }
  const disabled = busy || running || uncertain;
  return (
    <section
      className="panel updates-panel"
      aria-label={tr("updates.title")}
      data-updates-panel
    >
      <div className="section-title">
        <h2>{tr("updates.title")}</h2>
        <button
          className="secondary"
          type="button"
          disabled={busy}
          onClick={() => void refresh(true)}
        >
          {tr("updates.refresh")}
        </button>
      </div>
      <p>{tr("updates.description")}</p>
      {!status && !error && <p role="status">{tr("updates.loading")}</p>}
      {status && !status.enabled && (
        <p className="notice" data-updates-disabled>
          {tr("updates.disabled")}
        </p>
      )}
      {status?.enabled && (
        <>
          <dl className="updates-details">
            <div>
              <dt>{tr("updates.current")}</dt>
              <dd>
                <code>
                  {status.current_commit
                    ? status.current_commit.slice(0, 12)
                    : tr("updates.unknown")}
                </code>
              </dd>
            </div>
            <div>
              <dt>{tr("updates.status")}</dt>
              <dd role="status" data-update-state={status.state}>
                {tr("updates.state." + status.state)}
              </dd>
            </div>
            {status.phase && (
              <div>
                <dt>{tr("updates.phase")}</dt>
                <dd>
                  {tr(phaseKeys[status.phase] || "updates.phase.working")}
                </dd>
              </div>
            )}
            {status.candidate_commit && (
              <div>
                <dt>{tr("updates.candidate")}</dt>
                <dd>
                  <code title={status.candidate_commit}>
                    {status.candidate_commit.slice(0, 12)}
                  </code>
                </dd>
              </div>
            )}
          </dl>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void command("check");
            }}
            className="updates-check"
          >
            <label>
              {tr("updates.ref")}
              <input
                value={ref}
                maxLength={121}
                onChange={(event) => setRef(event.target.value)}
                disabled={disabled || status.state === "recovery_required"}
                autoComplete="off"
                spellCheck={false}
                required
              />
            </label>
            <button
              type="submit"
              disabled={
                disabled || status.state === "recovery_required" || !ref.trim()
              }
            >
              {tr("updates.check")}
            </button>
          </form>
          <p className="muted">{tr("updates.refHelp")}</p>
          {status.state === "ready" && status.candidate_commit && (
            <div className="updates-confirm" data-update-confirm>
              <p>{tr("updates.applyHelp")}</p>
              <code className="updates-identity">
                {status.candidate_commit}
              </code>
              <label>
                {tr("updates.confirmCommit")}
                <input
                  value={confirmation}
                  onChange={(event) => setConfirmation(event.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                  disabled={disabled}
                  maxLength={40}
                />
              </label>
              <button
                type="button"
                disabled={disabled || !updateCanApply(status, confirmation)}
                onClick={() => void command("apply")}
              >
                {tr("updates.apply")}
              </button>
            </div>
          )}
          {status.state === "recovery_required" && (
            <div className="notice updates-recovery" data-update-recovery>
              <p>{tr("updates.recoveryHelp")}</p>
              {status.recovery_available && status.operation_id ? (
                <>
                  <code className="updates-identity">
                    {status.operation_id}
                  </code>
                  <label>
                    {tr("updates.confirmRecovery")}
                    <input
                      value={confirmation}
                      onChange={(event) => setConfirmation(event.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                      disabled={disabled}
                      maxLength={80}
                    />
                  </label>
                  <button
                    type="button"
                    className="secondary danger"
                    disabled={
                      disabled || !updateCanRecover(status, confirmation)
                    }
                    onClick={() => void command("recover")}
                  >
                    {tr("updates.recover")}
                  </button>
                </>
              ) : (
                <p>{tr("updates.operatorRecovery")}</p>
              )}
            </div>
          )}
          {running && <p>{tr("updates.runningHelp")}</p>}
          {paused && <p className="notice">{tr("updates.pollPaused")}</p>}
        </>
      )}
      {uncertain && <p className="notice">{tr("updates.reconcile")}</p>}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
    </section>
  );
}
