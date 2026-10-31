// Workspace ordering supplements (and never replaces) api.ts session isolation.
// A project revision does not version roles or cross-project derived capacity.
export type WorkspaceIntent = { current: () => boolean };
export type WorkspaceRead = WorkspaceIntent & {
  signal: AbortSignal;
  cancel: () => void;
  finish: () => void;
};
export type WorkspaceOperation = WorkspaceIntent & { finish: () => void };
type VersionedProject = { id: string; revision: number };

export class WorkspaceRefresh {
  private alive = true;
  private generation = 0;
  private readGeneration = 0;
  private sequence = 0;
  private chosen = "";
  private projectRead = 0;
  private listRead = 0;
  private employeeRead = 0;
  private operation = 0;
  private revisions = new Map<string, number>();
  private reads = new Set<WorkspaceRead>();
  private foreground = new Set<WorkspaceRead>();
  private backgroundRead: WorkspaceRead | null = null;
  private background: object | null = null;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private run: (() => Promise<void>) | undefined;
  private eligible: (() => boolean) | undefined;
  private wasEligible = false;
  private catchUp = false;
  private fresh = false;
  private disconnect: (() => void) | undefined;
  constructor(private sessionCurrent: () => boolean) {}
  get projectID() {
    return this.chosen;
  }
  get writing() {
    return this.operation !== 0;
  }
  capture(): WorkspaceIntent {
    const generation = this.generation;
    return {
      current: () =>
        this.alive && this.sessionCurrent() && generation === this.generation,
    };
  }
  select(id: string) {
    this.invalidateReads();
    this.generation++;
    this.operation = 0;
    this.chosen = id;
    // A superseded transport may ignore abort. It cannot stall another selection.
    this.background = null;
    this.backgroundRead = null;
    this.catchUp = false;
    this.fresh = false;
    this.wasEligible = false;
    this.clearTimer();
    return this.capture();
  }
  read(family: "project" | "lists", background = false): WorkspaceRead {
    const intent = this.capture(),
      generation = this.readGeneration,
      sequence = ++this.sequence,
      controller = new AbortController();
    if (family === "project") this.projectRead = sequence;
    else this.listRead = sequence;
    // Both response families carry employees: ordering is shared across families.
    this.employeeRead = sequence;
    let cancelled = false;
    const ticket: WorkspaceRead = {
      signal: controller.signal,
      current: () =>
        !cancelled &&
        intent.current() &&
        generation === this.readGeneration &&
        this.employeeRead === sequence &&
        (family === "project" ? this.projectRead : this.listRead) === sequence,
      cancel: () => {
        cancelled = true;
        controller.abort();
        this.foreground.delete(ticket);
      },
      finish: () => {
        this.reads.delete(ticket);
        this.foreground.delete(ticket);
        if (this.backgroundRead === ticket) this.backgroundRead = null;
        this.updatePolling();
      },
    };
    for (const old of this.reads) old.cancel();
    this.reads.add(ticket);
    if (background) this.backgroundRead = ticket;
    else this.foreground.add(ticket);
    this.updatePolling();
    return ticket;
  }
  beginOperation(id: string): WorkspaceOperation | null {
    if (this.operation || id !== this.chosen || !this.capture().current())
      return null;
    this.invalidateReads();
    this.clearTimer();
    this.catchUp = true;
    const intent = this.capture(),
      operation = ++this.sequence;
    this.operation = operation;
    return {
      current: () => intent.current() && this.operation === operation,
      finish: () => {
        if (intent.current() && this.operation === operation) {
          this.operation = 0;
          this.updatePolling();
        }
      },
    };
  }
  accept(ticket: WorkspaceIntent, project: VersionedProject) {
    if (
      !ticket.current() ||
      project.id !== this.chosen ||
      project.revision < (this.revisions.get(project.id) ?? 0)
    )
      return false;
    this.revisions.set(project.id, project.revision);
    return true;
  }
  private invalidateReads() {
    this.readGeneration++;
    for (const read of this.reads) read.cancel();
    this.reads.clear();
  }
  pause() {
    this.clearTimer();
    this.catchUp = true;
    this.wasEligible = false;
    this.backgroundRead?.cancel();
  }
  markFresh() {
    this.fresh = true;
  }
  connectPolling(run: () => Promise<void>, eligible: () => boolean) {
    this.run = run;
    this.eligible = eligible;
    let hidden = document.hidden,
      online = navigator.onLine !== false;
    const changed = () => {
      const nextHidden = document.hidden,
        nextOnline = navigator.onLine !== false;
      if ((hidden && !nextHidden) || (!online && nextOnline))
        this.catchUp = true;
      hidden = nextHidden;
      online = nextOnline;
      this.updatePolling();
    };
    document.addEventListener("visibilitychange", changed);
    window.addEventListener("online", changed);
    window.addEventListener("offline", changed);
    // Nested dialogs (Bug Report, participation and members) live outside the
    // Workspace state. Observe only availability transitions, never page content.
    const observer = new MutationObserver(changed);
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["open"],
    });
    this.disconnect = () => {
      document.removeEventListener("visibilitychange", changed);
      window.removeEventListener("online", changed);
      window.removeEventListener("offline", changed);
      observer.disconnect();
    };
    this.updatePolling();
  }
  private available() {
    return (
      this.alive &&
      this.sessionCurrent() &&
      !this.writing &&
      !this.foreground.size &&
      !!this.chosen &&
      !document.hidden &&
      navigator.onLine !== false &&
      !!this.eligible?.() &&
      !document.querySelector("dialog[open]")
    );
  }
  updatePolling() {
    if (!this.run) return;
    if (!this.available()) {
      if (this.wasEligible) this.pause();
      this.clearTimer();
      this.wasEligible = false;
      return;
    }
    const resumed = !this.wasEligible;
    this.wasEligible = true;
    if (this.fresh) {
      this.fresh = false;
      this.catchUp = false;
      this.clearTimer();
      this.schedule();
      return;
    }
    if (resumed && this.catchUp) {
      void this.refreshBackground();
      return;
    }
    this.schedule();
  }
  private clearTimer() {
    if (this.timer !== undefined) {
      window.clearTimeout(this.timer);
      this.timer = undefined;
    }
  }
  private schedule() {
    if (this.timer !== undefined || this.background || !this.available())
      return;
    this.timer = window.setTimeout(() => {
      this.timer = undefined;
      void this.refreshBackground();
    }, 5000);
  }
  private async refreshBackground() {
    if (!this.available()) return;
    if (this.background) {
      this.catchUp = true;
      return;
    }
    this.clearTimer();
    this.catchUp = false;
    const batch = {};
    this.background = batch;
    try {
      await this.run?.();
    } catch {
      /* Background failure leaves the accepted snapshot intact. */
    } finally {
      if (this.background === batch) {
        this.background = null;
        if (this.catchUp && this.available()) void this.refreshBackground();
        else this.schedule();
      }
    }
  }
  close() {
    this.alive = false;
    this.invalidateReads();
    this.clearTimer();
    this.disconnect?.();
    this.background = null;
  }
}
