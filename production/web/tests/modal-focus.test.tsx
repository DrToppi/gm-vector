import { act, fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Modal } from "../src/Modal";
import { tr } from "../src/i18n";
import { renderReady, scenario } from "./fixtures";

describe("modal focus boundaries", () => {
  scenario();
  beforeEach(() => {
    // DOM-only geometry/showModal adapters exercise the boundary handlers.
    // Genuine native keyboard/modality evidence belongs to the three engines.
    vi.spyOn(HTMLElement.prototype, "getClientRects").mockImplementation(
      function (this: HTMLElement) {
        return (this.closest("[data-no-layout]")
          ? []
          : [{}]) as unknown as DOMRectList;
      },
    );
    vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
      function (this: HTMLDialogElement) {
        this.setAttribute("open", "");
        this.querySelector<HTMLElement>(
          "[data-modal-focus-guard='start']",
        )!.focus();
      },
    );
  });
  function guard(edge: "start" | "end") {
    return document.querySelector<HTMLElement>(
      `[data-modal-focus-guard='${edge}']`,
    )!;
  }

  it("preserves an already focused child when showing the dialog reaches its leading guard", async () => {
    await renderReady(
      <Modal title="Synthetic dialog" onClose={vi.fn()}>
        <input
          aria-label="Initial draft"
          defaultValue="Preserved draft"
          ref={(element) => {
            // Model an existing focus target, not native autofocus behaviour.
            // jsdom has no top layer; genuine initial focus is a browser oracle.
            if (element) {
              element.closest("dialog")!.style.display = "block";
              element.focus();
            }
          }}
        />
        <button>Last action</button>
      </Modal>,
    );
    expect(document.activeElement).toBe(screen.getByLabelText("Initial draft"));
  });

  it("starts at the first real control and wraps both boundaries without changing the draft", async () => {
    await renderReady(
      <Modal title="Synthetic dialog" onClose={vi.fn()}>
        <input aria-label="Draft" defaultValue="Unsaved" />
        <button>Last action</button>
      </Modal>,
    );
    const first = screen.getByRole("button", { name: tr("ma7a4033657") });
    const last = screen.getByRole("button", { name: "Last action" });
    const draft = screen.getByLabelText("Draft") as HTMLInputElement;
    expect(document.activeElement).toBe(first);
    fireEvent.change(draft, { target: { value: "Unsaved changed" } });
    act(() => last.focus());
    expect(fireEvent.keyDown(last, { key: "Tab" })).toBe(true);
    act(() => guard("end").focus());
    expect(document.activeElement).toBe(first);
    expect(fireEvent.keyDown(first, { key: "Tab", shiftKey: true })).toBe(true);
    act(() => guard("start").focus());
    expect(document.activeElement).toBe(last);
    expect(draft.value).toBe("Unsaved changed");
  });

  it("recomputes visible enabled candidates and positive-tabindex order at each boundary", async () => {
    const content = (disabled: boolean) => (
      <Modal title="Synthetic dialog" onClose={vi.fn()}>
        <button tabIndex={2}>Second priority</button>
        <button tabIndex={1} disabled={disabled}>
          First priority
        </button>
        <button hidden>Hidden</button>
        <button style={{ visibility: "hidden" }}>Invisible</button>
        <fieldset disabled>
          <button>Disabled fieldset</button>
        </fieldset>
        <button tabIndex={-1}>Programmatic only</button>
        <details>
          <summary>Closed details</summary>
          <button data-no-layout>Collapsed</button>
        </details>
        <button data-no-layout>Without layout</button>
        <button disabled>Disabled last</button>
      </Modal>
    );
    const view = await renderReady(content(false));
    const first = screen.getByRole("button", { name: "First priority" });
    expect(document.activeElement).toBe(first);
    expect(guard("start").tabIndex).toBe(1);
    act(() => guard("start").focus());
    expect(document.activeElement).toBe(screen.getByText("Closed details"));
    view.rerender(content(true));
    act(() => guard("end").focus());
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Second priority" }),
    );
    expect(guard("start").tabIndex).toBe(2);
  });

  it.each([false, true])(
    "preserves cancel, backdrop and explicit close rules, dismissible=%s",
    async (dismissible) => {
      const onClose = vi.fn();
      const view = await renderReady(
        <Modal
          title="Synthetic dialog"
          onClose={onClose}
          dismissible={dismissible}
        >
          <input aria-label="Draft" defaultValue="Keep" />
        </Modal>,
      );
      const dialog = view.container.querySelector("dialog")!;
      fireEvent.click(screen.getByLabelText("Draft"));
      expect(onClose).not.toHaveBeenCalled();
      const cancel = new Event("cancel", { cancelable: true });
      fireEvent(dialog, cancel);
      expect(cancel.defaultPrevented).toBe(true);
      fireEvent.click(dialog);
      expect(onClose).toHaveBeenCalledTimes(dismissible ? 2 : 0);
      fireEvent.click(screen.getByRole("button", { name: tr("ma7a4033657") }));
      expect(onClose).toHaveBeenCalledTimes(dismissible ? 3 : 1);
      view.unmount();
      expect(dialog.open).toBe(false);
    },
  );
});
