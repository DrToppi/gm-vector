export const HISTORY_BACK_POSITIONS = 100;
export type HistoryPosition = {
  revision: number;
  cursor: string | null;
  trail: (string | null)[];
  reload: number;
  discarded: boolean;
};
export function firstHistoryPosition(
  revision: number,
  reload = 0,
): HistoryPosition {
  return { revision, cursor: null, trail: [], reload, discarded: false };
}
export function nextHistoryPosition(
  position: HistoryPosition,
  next: string,
  revision: number,
): HistoryPosition {
  const trail = [
    ...(position.revision === revision ? position.trail : []),
    position.revision === revision ? position.cursor : null,
  ];
  return {
    ...position,
    revision,
    cursor: next,
    trail: trail.slice(-HISTORY_BACK_POSITIONS),
    discarded: position.discarded || trail.length > HISTORY_BACK_POSITIONS,
  };
}
export function previousHistoryPosition(
  position: HistoryPosition,
): HistoryPosition {
  if (!position.trail.length) return position;
  const trail = position.trail.slice(),
    cursor = trail.pop() ?? null;
  return { ...position, trail, cursor };
}
