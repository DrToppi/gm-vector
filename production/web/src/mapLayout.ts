import { tr } from "./i18n";
import type { Project } from "./types";

export const PROJECT_ROOT = "__project_root__";
// Bilateral subtree layout from main:index.html. Dependencies affect Trace,
// not the hierarchy geometry. All coordinates remain transient UI state.
export function mapLayout(
  project: Project,
  _expanded: { from: string; to: string }[],
  visible: string[],
  manual: Record<string, { x: number; y: number }>,
) {
  const shown = new Set(visible),
    tasks = project.tasks.filter((t) => shown.has(t.id));
  const children = (id: string) => tasks.filter((t) => t.parent === id);
  const roots = tasks.filter((t) => !shown.has(t.parent));
  const spans = new Map<string, number>(),
    W = 290,
    H = 104,
    GAP = 150,
    STEP = 164;
  const measure = (id: string): number => {
    const span = Math.max(
      STEP,
      children(id).reduce((n, t) => n + measure(t.id), 0),
    );
    spans.set(id, span);
    return span;
  };
  roots.forEach((t) => measure(t.id));
  const sides = [
    roots.filter((_, i) => i % 2 === 0),
    roots.filter((_, i) => i % 2 === 1),
  ];
  const totals = sides.map((ts) =>
    ts.reduce((n, t) => n + spans.get(t.id)!, 0),
  );
  const height = Math.max(STEP, ...totals);
  const result = new Map<
    string,
    { x: number; y: number; w: number; h: number }
  >();
  result.set(PROJECT_ROOT, { x: 0, y: height / 2 - H / 2, w: W, h: H });
  const place = (id: string, depth: number, side: number, top: number) => {
    const span = spans.get(id)!;
    result.set(id, {
      x: side * depth * (W + GAP),
      y: top + span / 2 - H / 2,
      w: W,
      h: H,
    });
    let y = top;
    for (const t of children(id)) {
      place(t.id, depth + 1, side, y);
      y += spans.get(t.id)!;
    }
  };
  sides.forEach((ts, i) => {
    let y = (height - totals[i]) / 2;
    for (const t of ts) {
      place(t.id, 1, i === 0 ? 1 : -1, y);
      y += spans.get(t.id)!;
    }
  });
  const minX = Math.min(...[...result.values()].map((b) => b.x));
  for (const [id, b] of result) {
    b.x += 40 - minX;
    b.y += 40;
    if (manual[id]) {
      b.x = Math.max(10, manual[id].x);
      b.y = Math.max(10, manual[id].y);
    }
  }
  return result;
}

export type MapBox = { x: number; y: number; w: number; h: number };
export type ManualPositions = Record<string, { x: number; y: number }>;
export type StageBand = {
  id: string;
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
};
const CARD_W = 290,
  CARD_H = 104,
  ROW = 164,
  COLUMN = 440;
function applyManual(boxes: Map<string, MapBox>, manual: ManualPositions) {
  for (const [id, box] of boxes) {
    const position = manual[id];
    if (
      position &&
      Number.isFinite(position.x) &&
      Number.isFinite(position.y)
    ) {
      box.x = Math.max(10, position.x);
      box.y = Math.max(10, position.y);
    }
  }
  return boxes;
}

// Rank dependencies only. Folding can make a cyclic quotient of a valid leaf
// DAG, so strongly connected display nodes share a column instead of recursing.
export function flowLayout(
  project: Project,
  edges: { from: string; to: string }[],
  visible: string[],
  manual: ManualPositions,
) {
  const shown = new Set(visible),
    ids = project.tasks.filter((t) => shown.has(t.id)).map((t) => t.id);
  const order = new Map(ids.map((id, index) => [id, index]));
  const adjacent = new Map(ids.map((id) => [id, new Set<string>()]));
  for (const edge of edges)
    if (
      adjacent.has(edge.from) &&
      adjacent.has(edge.to) &&
      edge.from !== edge.to
    )
      adjacent.get(edge.from)!.add(edge.to);
  const number = new Map<string, number>(),
    low = new Map<string, number>(),
    stack: string[] = [],
    onStack = new Set<string>(),
    components: string[][] = [];
  let next = 0;
  const visit = (id: string) => {
    number.set(id, next);
    low.set(id, next++);
    stack.push(id);
    onStack.add(id);
    for (const to of [...adjacent.get(id)!].sort(
      (a, b) => order.get(a)! - order.get(b)!,
    )) {
      if (!number.has(to)) {
        visit(to);
        low.set(id, Math.min(low.get(id)!, low.get(to)!));
      } else if (onStack.has(to))
        low.set(id, Math.min(low.get(id)!, number.get(to)!));
    }
    if (low.get(id) === number.get(id)) {
      const members: string[] = [];
      let member: string;
      do {
        member = stack.pop()!;
        onStack.delete(member);
        members.push(member);
      } while (member !== id);
      components.push(members.sort((a, b) => order.get(a)! - order.get(b)!));
    }
  };
  for (const id of ids) if (!number.has(id)) visit(id);
  components.sort((a, b) => order.get(a[0])! - order.get(b[0])!);
  const componentOf = new Map<string, number>();
  components.forEach((group, index) =>
    group.forEach((id) => componentOf.set(id, index)),
  );
  const outgoing = components.map(() => new Set<number>()),
    incoming = components.map(() => 0),
    ranks = components.map(() => 0);
  for (const [from, targets] of adjacent)
    for (const to of targets) {
      const a = componentOf.get(from)!,
        b = componentOf.get(to)!;
      if (a !== b && !outgoing[a].has(b)) {
        outgoing[a].add(b);
        incoming[b]++;
      }
    }
  const queue = incoming.flatMap((count, index) =>
    count === 0 ? [index] : [],
  );
  while (queue.length) {
    const from = queue.shift()!;
    for (const to of outgoing[from]) {
      ranks[to] = Math.max(ranks[to], ranks[from] + 1);
      if (--incoming[to] === 0) {
        queue.push(to);
        queue.sort((a, b) => a - b);
      }
    }
  }
  const boxes = new Map<string, MapBox>();
  boxes.set(PROJECT_ROOT, { x: 40, y: 40, w: CARD_W, h: CARD_H });
  const rows = new Map<number, number>();
  for (const id of ids) {
    const column = ranks[componentOf.get(id)!],
      row = rows.get(column) || 0;
    rows.set(column, row + 1);
    boxes.set(id, {
      x: 40 + (column + 1) * COLUMN,
      y: 40 + row * ROW,
      w: CARD_W,
      h: CARD_H,
    });
  }
  return applyManual(boxes, manual);
}

export function stageLayout(
  project: Project,
  visible: string[],
  manual: ManualPositions,
): { boxes: Map<string, MapBox>; bands: StageBand[] } {
  const shown = new Set(visible),
    byID = new Map(project.tasks.map((t) => [t.id, t]));
  const allChildren = new Set(project.tasks.map((t) => t.parent));
  const roots = project.tasks.filter((t) => !t.parent && allChildren.has(t.id));
  const groups = roots.map((t) => ({
    id: t.id,
    title: t.title,
    ids: [] as string[],
  }));
  const ungrouped = {
    id: "__stage_ungrouped__",
    title: tr("m3387dfa598"),
    ids: [] as string[],
  };
  const groupByID = new Map(groups.map((g) => [g.id, g]));
  for (const t of project.tasks) {
    if (!shown.has(t.id)) continue;
    let root = t,
      seen = new Set<string>();
    while (root.parent && byID.has(root.parent) && !seen.has(root.id)) {
      seen.add(root.id);
      root = byID.get(root.parent)!;
    }
    (groupByID.get(root.id) || ungrouped).ids.push(t.id);
  }
  if (ungrouped.ids.length) groups.push(ungrouped);
  const boxes = new Map<string, MapBox>();
  boxes.set(PROJECT_ROOT, { x: 40, y: 40, w: CARD_W, h: CARD_H });
  const bands: StageBand[] = [];
  let columnX = 40;
  for (const group of groups.filter((g) => g.ids.length)) {
    const members = new Set(group.ids),
      ordered: string[] = [],
      depths = new Map<string, number>();
    const walk = (id: string, depth: number) => {
      ordered.push(id);
      depths.set(id, depth);
      for (const t of project.tasks)
        if (t.parent === id && members.has(t.id)) walk(t.id, depth + 1);
    };
    for (const id of group.ids)
      if (!members.has(byID.get(id)!.parent)) walk(id, 0);
    const indent = (id: string) => Math.min(4, depths.get(id) || 0) * 18;
    const bandWidth = CARD_W + Math.max(0, ...ordered.map(indent)) + 28;
    ordered.forEach((id, row) =>
      boxes.set(id, {
        x: columnX + 14 + indent(id),
        y: 220 + row * ROW,
        w: CARD_W,
        h: CARD_H,
      }),
    );
    bands.push({
      id: group.id,
      title: group.title,
      x: columnX,
      y: 180,
      w: bandWidth,
      h: Math.max(ROW, ordered.length * ROW) + 18,
    });
    columnX += bandWidth + 110;
  }
  applyManual(boxes, manual);
  // Keep manually moved members visibly associated with their stage.
  for (const band of bands) {
    const group = groups.find((g) => g.id === band.id)!;
    for (const id of group.ids) {
      const b = boxes.get(id)!;
      const right = Math.max(band.x + band.w, b.x + b.w + 14),
        bottom = Math.max(band.y + band.h, b.y + b.h + 20);
      band.x = Math.min(band.x, b.x - 14);
      band.y = Math.min(band.y, b.y - 40);
      band.w = right - band.x;
      band.h = bottom - band.y;
    }
  }
  return { boxes, bands };
}
