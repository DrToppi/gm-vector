import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach, expect, vi } from "vitest";

type ConsoleMessage = { level: "error" | "warn"; message: string };
let observed: ConsoleMessage[] = [];
let expected: ConsoleMessage[] = [];

// Exceptional console output must be expected exactly by the individual test.
// There is no suite-wide suppression or pattern that hides React diagnostics.
export function expectConsoleMessage(
  level: ConsoleMessage["level"],
  message: string,
) {
  expected.push({ level, message });
}

const actEnvironment = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean;
};
const previousActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT;
beforeAll(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true;
});
afterAll(() => {
  if (previousActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT;
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = previousActEnvironment;
  }
});

beforeEach(() => {
  observed = [];
  expected = [];
  for (const level of ["error", "warn"] as const) {
    vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
      observed.push({ level, message: args.map(String).join(" ") });
    });
  }
});

afterEach(() => {
  let remainingTimers = 0;
  try {
    cleanup();
    if (vi.isFakeTimers()) remainingTimers = vi.getTimerCount();
  } finally {
    if (vi.isFakeTimers()) {
      vi.clearAllTimers();
      vi.useRealTimers();
    }
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  }
  expect(remainingTimers, "Test left timers after component cleanup").toBe(0);
  expect(observed, "Unexpected or missing console diagnostics").toEqual(expected);
});
