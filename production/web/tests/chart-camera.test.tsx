import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Chart } from "../src/Chart";
import { project, renderReady, scenario } from "./fixtures";

describe("map camera guarded effects", () => {
  scenario();

  it("retains an adjusted camera through unrelated project and selection rerenders", async () => {
    // Deterministic DOM dimensions establish only hook/camera retention. Native
    // layout, wheel and pointer behavior remain real-browser acceptance checks.
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(960);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(640);
    const observers: Observer[] = [];
    class Observer {
      observe = vi.fn();
      disconnect = vi.fn();
      constructor() {
        observers.push(this);
      }
    }
    vi.stubGlobal("ResizeObserver", Observer);
    const p = project();
    const props = {
      project: p,
      view: "map" as const,
      selected: "",
      trace: "",
      filter: "",
      canEdit: true,
      setSelected: vi.fn(),
      setTrace: vi.fn(),
      onEdit: vi.fn(),
      onChild: vi.fn(),
      onLink: vi.fn(),
      onChange: vi.fn(),
    };
    const view = await renderReady(<Chart {...props} />);
    const camera = () =>
      view.container
        .querySelector("[data-map-content]")!
        .getAttribute("transform");
    const fitted = camera();
    fireEvent.click(screen.getByRole("button", { name: "+" }));
    const adjusted = camera();
    expect(adjusted).not.toBe(fitted);
    view.rerender(
      <Chart
        {...props}
        project={{ ...p, revision: 2, brief: "An unrelated accepted update" }}
        selected="task-1"
        canEdit={false}
      />,
    );
    expect(camera()).toBe(adjusted);
    expect(observers).toHaveLength(1);
    expect(props.onChange).not.toHaveBeenCalled();
    view.unmount();
    expect(observers[0].disconnect).toHaveBeenCalledTimes(1);
  });
});
