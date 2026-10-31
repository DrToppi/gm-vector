import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { HistoryExportActions } from "../src/HistoryExportActions";
import { advanceSession } from "../src/api";
import { historyFilters } from "../src/history";
import { historyExport, historyItem, reply, scenario } from "./fixtures";

function printWindow() {
  const state = { closed: false };
  const document = { open: vi.fn(), write: vi.fn(), close: vi.fn() };
  const window = {
    get closed() {
      return state.closed;
    },
    opener: {} as unknown,
    document,
    focus: vi.fn(),
    print: vi.fn(),
    close: vi.fn(() => {
      state.closed = true;
    }),
  };
  return window;
}

describe("owned history print windows", () => {
  const current = scenario();
  const path = "/projects/A/time-history/export";
  const props = { projectID: "A", filters: historyFilters(), revision: 1 };

  it.each(["context", "unmount", "session"] as const)(
    "retains every successful window and closes all of them on %s",
    async (reason) => {
      vi.useFakeTimers();
      const f = current();
      f.hold("GET", path);
      const first = printWindow(),
        second = printWindow();
      vi.spyOn(window, "open")
        .mockReturnValueOnce(first as unknown as Window)
        .mockReturnValueOnce(second as unknown as Window);
      const view = render(<HistoryExportActions {...props} />);
      fireEvent.click(screen.getByRole("button", { name: "PDF" }));
      expect(window.open).toHaveBeenCalledTimes(1);
      expect(first.document.write).not.toHaveBeenCalled();
      await reply(f.latest("GET", path), historyExport());
      await act(async () => vi.advanceTimersByTimeAsync(150));
      expect(first.print).toHaveBeenCalledTimes(1);
      expect(first.opener).toBeNull();
      expect(first.closed).toBe(false);
      fireEvent.click(screen.getByRole("button", { name: "PDF" }));
      await reply(f.latest("GET", path), historyExport());
      await act(async () => vi.advanceTimersByTimeAsync(150));
      expect(second.print).toHaveBeenCalledTimes(1);
      if (reason === "context")
        view.rerender(<HistoryExportActions {...props} revision={2} />);
      else if (reason === "unmount") view.unmount();
      else
        await act(async () => {
          advanceSession();
        });
      expect(first.closed).toBe(true);
      expect(second.closed).toBe(true);
      view.unmount();
    },
  );

  it.each(["context", "unmount", "session"] as const)(
    "closes an outstanding popup on %s before any late write",
    async (reason) => {
      vi.useFakeTimers();
      const f = current();
      f.hold("GET", path);
      const popup = printWindow();
      vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
      const view = render(<HistoryExportActions {...props} />);
      fireEvent.click(screen.getByRole("button", { name: "PDF" }));
      const pending = f.latest("GET", path);
      if (reason === "context")
        view.rerender(<HistoryExportActions {...props} revision={2} />);
      else if (reason === "unmount") view.unmount();
      else
        await act(async () => {
          advanceSession();
        });
      expect(popup.closed).toBe(true);
      expect(pending.signal?.aborted).toBe(true);
      await reply(pending, historyExport());
      await act(async () => vi.advanceTimersByTimeAsync(150));
      expect(popup.document.write).not.toHaveBeenCalled();
      expect(popup.print).not.toHaveBeenCalled();
      view.unmount();
    },
  );

  it("closes a popup on a server error and sends no request if the browser blocks opening", async () => {
    const f = current();
    f.hold("GET", path);
    const popup = printWindow();
    vi.spyOn(window, "open")
      .mockReturnValueOnce(popup as unknown as Window)
      .mockReturnValueOnce(null);
    const view = render(<HistoryExportActions {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "PDF" }));
    await reply(
      f.latest("GET", path),
      {
        error: {
          code: "history_export_busy",
          message: "Synthetic busy export",
        },
      },
      429,
    );
    expect(popup.closed).toBe(true);
    expect(popup.print).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "PDF" }));
    expect(f.find("GET", path)).toHaveLength(1);
    view.unmount();
  });

  it("closes its popup when a legal server response exceeds the local PDF conversion budget", async () => {
    const f = current();
    f.hold("GET", path);
    const popup = printWindow();
    vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
    const view = render(<HistoryExportActions {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "PDF" }));
    await reply(
      f.latest("GET", path),
      historyExport("A", {
        total_count: 10001,
        items: Array.from({ length: 10001 }, (_, index) =>
          historyItem("entry-" + index),
        ),
      }),
    );
    expect(popup.closed).toBe(true);
    expect(popup.document.write).not.toHaveBeenCalled();
    expect(popup.print).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toBeTruthy();
    view.unmount();
  });
});
