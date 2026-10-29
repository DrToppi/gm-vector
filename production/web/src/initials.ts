// Accepted name format: family name first, given name second, patronymic optional.
const graphemes = new Intl.Segmenter(undefined, {granularity: 'grapheme'});
export function initials(name?: string | null): string {
  const parts = name?.trim().normalize('NFC').split(/\s+/u).filter(Boolean) ?? [];
  if (!parts.length) return '?';
  const first = (word: string) => graphemes.segment(word)[Symbol.iterator]().next().value!.segment.toUpperCase();
  return parts.length > 1 ? first(parts[1]) + first(parts[0]) : first(parts[0]);
}
