export const updateStates = [
  "idle",
  "checking",
  "ready",
  "updating",
  "succeeded",
  "failed",
  "recovery_required",
  "recovering",
] as const;
export type UpdateState = (typeof updateStates)[number];
export type UpdateStatus = {
  enabled: boolean;
  state: UpdateState | "disabled";
  phase: string;
  current_commit: string;
  candidate_commit: string;
  operation_id: string;
  recovery_available: boolean;
};
export const isCommit = (value: unknown): value is string =>
  typeof value === "string" && /^[a-f0-9]{40}$/.test(value);
export const isOperation = (value: unknown): value is string =>
  typeof value === "string" && /^[a-zA-Z0-9_-]{1,80}$/.test(value);
export function updateStatus(value: unknown): UpdateStatus {
  if (!value || typeof value !== "object") throw Error("invalid update status");
  const v = value as Record<string, unknown>;
  if (v.enabled === false)
    return {
      enabled: false,
      state: "disabled",
      phase: "",
      current_commit: "",
      candidate_commit: "",
      operation_id: "",
      recovery_available: false,
    };
  if (v.enabled !== true || !updateStates.includes(v.state as UpdateState))
    throw Error("invalid update state");
  return {
    enabled: true,
    state: v.state as UpdateState,
    phase:
      typeof v.phase === "string" && /^[a-z_]{0,40}$/.test(v.phase)
        ? v.phase
        : "",
    current_commit: isCommit(v.current_commit) ? v.current_commit : "",
    candidate_commit: isCommit(v.candidate_commit) ? v.candidate_commit : "",
    operation_id: isOperation(v.operation_id) ? v.operation_id : "",
    recovery_available: v.recovery_available === true,
  };
}
export const updateRunning = (state: UpdateStatus["state"]) =>
  state === "checking" || state === "updating" || state === "recovering";
export function updateCanApply(
  status: UpdateStatus | null,
  confirmation: string,
) {
  return (
    !!status?.enabled &&
    status.state === "ready" &&
    isCommit(status.candidate_commit) &&
    confirmation === status.candidate_commit
  );
}
export function updateCanRecover(
  status: UpdateStatus | null,
  confirmation: string,
) {
  return (
    !!status?.enabled &&
    status.state === "recovery_required" &&
    status.recovery_available &&
    isOperation(status.operation_id) &&
    confirmation === status.operation_id
  );
}
// Shared by status reads and mutations: an older response cannot resurrect a
// candidate, operation or confirmation after a newer request or unmount.
export class UpdateRequests {
  private active = true;
  private generation = 0;
  private controller: AbortController | null = null;
  open() {
    this.active = true;
    this.invalidate();
  }
  invalidate() {
    this.generation++;
    this.controller?.abort();
    this.controller = null;
  }
  begin() {
    this.invalidate();
    const generation = this.generation;
    const controller = new AbortController();
    this.controller = controller;
    return {
      signal: controller.signal,
      current: () =>
        this.active &&
        generation === this.generation &&
        !controller.signal.aborted,
    };
  }
  close() {
    this.active = false;
    this.invalidate();
  }
}
