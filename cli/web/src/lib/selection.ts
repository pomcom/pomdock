/** Select only IDs in the current ordered view; anchors survive sorting safely. */
export function toggleSelection(ids: string[], selected: ReadonlySet<string>, id: string, anchor: string | null, shift = false): Set<string> {
  const next = new Set(selected);
  const end = ids.indexOf(id), start = anchor ? ids.indexOf(anchor) : -1;
  if (end < 0) return next;
  const range = shift && start >= 0 ? ids.slice(Math.min(start, end), Math.max(start, end) + 1) : [id];
  const adding = !next.has(id);
  for (const key of range) adding ? next.add(key) : next.delete(key);
  return next;
}

export function toggleVisible(ids: string[], selected: ReadonlySet<string>): Set<string> {
  const next = new Set(selected), remove = ids.length > 0 && ids.every(id => next.has(id));
  for (const id of ids) remove ? next.delete(id) : next.add(id);
  return next;
}

export function matchPositions(text: string, query: string): number[] {
  const term = query.trim().toLowerCase();
  if (!term) return [];
  const hay = text.toLowerCase(), positions: number[] = [];
  for (let at = hay.indexOf(term); at >= 0; at = hay.indexOf(term, at + term.length)) positions.push(at);
  return positions;
}
