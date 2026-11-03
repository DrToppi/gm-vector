import { useLayoutEffect, useRef } from "react";
import { tr } from "./i18n";

const guardStyle = {
  position: "fixed" as const,
  width: 1,
  height: 1,
  opacity: 0,
  pointerEvents: "none" as const,
};

function tabStops(dialog: HTMLDialogElement) {
  return [
    ...dialog.querySelectorAll<HTMLElement>(
      "button,input,select,textarea,summary,a[href],[tabindex],[contenteditable='true']",
    ),
  ]
    .filter(
      (element) =>
        !element.hasAttribute("data-modal-focus-guard") &&
        element.closest("dialog") === dialog &&
        element.tabIndex >= 0 &&
        !element.matches(":disabled") &&
        !element.closest("[hidden],[inert]") &&
        element.getClientRects().length > 0 &&
        getComputedStyle(element).visibility === "visible",
    )
    .sort((left, right) => {
      const a = left.tabIndex > 0;
      const b = right.tabIndex > 0;
      return a !== b ? (a ? -1 : 1) : a ? left.tabIndex - right.tabIndex : 0;
    });
}

function leadingOrder(
  dialog: HTMLDialogElement,
  guard: HTMLSpanElement | null,
) {
  if (guard) guard.tabIndex = tabStops(dialog)[0]?.tabIndex || 0;
}
export function Modal({
  title,
  children,
  onClose,
  dismissible = true,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
  dismissible?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const leading = useRef<HTMLSpanElement>(null);
  const navigation = useRef<"forward" | "backward" | null>(null);
  const opening = useRef(false);
  const initialFocus = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    const dialog = ref.current;
    if (dialog) {
      const active = document.activeElement;
      initialFocus.current =
        active instanceof HTMLElement && dialog.contains(active)
          ? active
          : null;
      opening.current = true;
      try {
        dialog.showModal();
        leadingOrder(dialog, leading.current);
      } finally {
        opening.current = false;
        initialFocus.current = null;
      }
    }
    return () => dialog?.close();
  }, []);
  function wrap(edge: "start" | "end", previous: EventTarget | null) {
    const dialog = ref.current;
    if (!dialog?.open) return;
    const stops = tabStops(dialog);
    leadingOrder(dialog, leading.current);
    const fromInside = previous instanceof Node && dialog.contains(previous);
    const atBoundary = fromInside || navigation.current !== null;
    const last = atBoundary ? edge === "start" : edge === "end";
    navigation.current = null;
    const target = opening.current
      ? initialFocus.current && stops.includes(initialFocus.current)
        ? initialFocus.current
        : stops[0]
      : last
        ? stops.at(-1)
        : stops[0];
    target?.focus();
  }
  return (
    <dialog
      ref={ref}
      onKeyDownCapture={(e) => {
        if (e.key === "Tab") {
          navigation.current = e.shiftKey ? "backward" : "forward";
          leadingOrder(e.currentTarget, leading.current);
        }
      }}
      onFocusCapture={(e) => {
        if (!(e.target as HTMLElement).hasAttribute("data-modal-focus-guard")) {
          navigation.current = null;
          leadingOrder(e.currentTarget, leading.current);
        }
      }}
      onPointerDownCapture={() => {
        navigation.current = null;
      }}
      onCancel={(e) => {
        e.preventDefault();
        if (dismissible) onClose();
      }}
      onClick={(e) => {
        if (dismissible && e.target === ref.current) onClose();
      }}
    >
      <span
        ref={leading}
        tabIndex={0}
        data-modal-focus-guard="start"
        style={guardStyle}
        onFocus={(e) => wrap("start", e.relatedTarget)}
      />
      <header>
        <h2>{title}</h2>
        <button
          type="button"
          className="icon"
          aria-label={tr("ma7a4033657")}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      {children}
      <span
        tabIndex={0}
        data-modal-focus-guard="end"
        style={guardStyle}
        onFocus={(e) => wrap("end", e.relatedTarget)}
      />
    </dialog>
  );
}
