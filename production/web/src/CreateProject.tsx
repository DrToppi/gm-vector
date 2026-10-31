import { useState } from "react";
import { tr } from "./i18n";
import { usePlanningClock } from "./PlanningClock";
import { useSessionScope } from "./useSessionScope";
import { MIN_PLANNING_DATE, MAX_PLANNING_DATE } from "./planningCalendar";
import type { Calendar } from "./types";
import { Modal } from "./Modal";
import { ProjectImport } from "./ProjectImport";
export function CreateProject({
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
