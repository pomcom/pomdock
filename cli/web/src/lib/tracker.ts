import type { ReportRow } from './types.ts';

// Pure tracker logic: turning atuin rows + the saved curation into editable
// items, diffing them back into a compact saved document, merging, noise rules
// and TSV export. No DOM or network here.

/** The editable report columns of one item. */
export type Fields = {
  incl: boolean;
  measure: string;
  ie: string; // "" | Internal | External
  source: string;
  tester: string;
  mitre: string;
  outcome: string;
  comment: string;
  targets: string;
};

export type Item = Fields & {
  key: string; // atuin id for a row, "m-…" for a merged item
  kind: 'row' | 'merged';
  ids: string[]; // atuin ids this item covers
  ts: number;
  endTs: number;
  started: string;
  ended: string;
  command: string;
  origMeasure: string;
  exit: number;
  noise: boolean;
  deleted: boolean;
  expanded: boolean;
};

export type Defaults = { tester: string; source: string };

type RowOverride = Partial<Fields> & { deleted?: boolean };
type SavedMerged = Omit<Item, 'kind' | 'expanded' | 'deleted' | 'noise'> & { deleted?: boolean };

/** What <loot>/.pomdock/tracker.json holds. Rows only store what differs from their defaults. */
export type SavedState = {
  version: 1;
  rows: Record<string, RowOverride>;
  merged: SavedMerged[];
};

const FIELD_KEYS: (keyof Fields)[] = ['incl', 'measure', 'ie', 'source', 'tester', 'mitre', 'outcome', 'comment', 'targets'];

export function rowDefaults(r: ReportRow, d: Defaults): Fields {
  // The measure is the tester's own short summary, written by hand. We only
  // pre-fill it when atuin carried a deliberate intent (`pdsession <intent>`),
  // which differs from the raw command; a plain command is left blank so it is
  // not mistaken for a measure. The full command always lives in the comment.
  return {
    incl: false, measure: r.measure === r.command ? '' : r.measure, ie: '', source: d.source, tester: d.tester,
    mitre: '', outcome: '', comment: r.command, targets: r.targets,
  };
}

export function rowItem(r: ReportRow, d: Defaults, override: RowOverride = {}): Item {
  const fields = rowDefaults(r, d);
  for (const k of FIELD_KEYS) {
    if (override[k] !== undefined) (fields as Record<string, unknown>)[k] = override[k];
  }
  return {
    ...fields, key: r.id, kind: 'row', ids: [r.id], ts: r.ts, endTs: r.endTs,
    started: r.started, ended: r.ended, command: r.command, origMeasure: r.measure,
    exit: r.exit, noise: r.noise, deleted: !!override.deleted, expanded: false,
  };
}

export function buildItems(rows: ReportRow[], saved: SavedState | null, d: Defaults): Item[] {
  const merged = saved?.merged ?? [];
  const absorbed = new Set(merged.flatMap((m) => m.ids));
  const items = rows.filter((r) => !absorbed.has(r.id)).map((r) => rowItem(r, d, saved?.rows?.[r.id]));
  for (const m of merged) {
    items.push({ ...m, kind: 'merged', noise: false, deleted: !!m.deleted, expanded: false });
  }
  return items;
}

export function serialize(items: Item[], rowsById: Map<string, ReportRow>, d: Defaults): SavedState {
  const state: SavedState = { version: 1, rows: {}, merged: [] };
  for (const it of items) {
    if (it.kind === 'merged') {
      const { kind: _k, expanded: _e, noise: _n, deleted, ...rest } = it;
      state.merged.push(deleted ? { ...rest, deleted } : rest);
      continue;
    }
    const row = rowsById.get(it.key);
    if (!row) continue;
    const base = rowDefaults(row, d);
    const o: RowOverride = {};
    for (const k of FIELD_KEYS) {
      if (it[k] !== base[k]) (o as Record<string, unknown>)[k] = it[k];
    }
    if (it.deleted) o.deleted = true;
    if (Object.keys(o).length) state.rows[it.key] = o;
  }
  return state;
}

/** Combine checked items into one report row spanning their time range. */
export function mergeItems(chk: Item[], d: Defaults): Item {
  const sorted = [...chk].sort((a, b) => a.ts - b.ts);
  const seen = new Set<string>();
  const targets: string[] = [];
  for (const it of sorted) {
    for (const t of it.targets.split(',').map((s) => s.trim()).filter(Boolean)) {
      if (!seen.has(t.toLowerCase())) {
        seen.add(t.toLowerCase());
        targets.push(t);
      }
    }
  }
  const first = (pick: (it: Item) => string) => sorted.map(pick).find(Boolean) ?? '';
  const measure = sorted.map((it) => it.measure).filter(Boolean).join('; ');
  return {
    key: `m-${sorted[0].ids[0]}-${Date.now().toString(36)}`,
    kind: 'merged',
    ids: sorted.flatMap((it) => it.ids),
    ts: sorted[0].ts,
    endTs: Math.max(...sorted.map((it) => it.endTs)),
    started: sorted[0].started,
    ended: sorted[sorted.length - 1].ended,
    measure,
    origMeasure: measure,
    command: sorted.map((it) => it.command).join('  |  '),
    targets: targets.join(', '),
    exit: 0,
    noise: false,
    incl: true,
    ie: first((it) => it.ie),
    source: first((it) => it.source) || d.source,
    tester: first((it) => it.tester) || d.tester,
    mitre: first((it) => it.mitre),
    outcome: first((it) => it.outcome),
    comment: sorted.map((it) => it.comment).filter(Boolean).join('\n\n'),
    deleted: false,
    expanded: false,
  };
}

/**
 * Compiles noise rules once into a predicate. A rule is a case-insensitive
 * substring, or /regex/ (case-insensitive); invalid regexes never match.
 */
export function noiseMatcher(rules: string[]): (cmd: string) => boolean {
  const subs: string[] = [];
  const res: RegExp[] = [];
  for (const r of rules) {
    if (!r) continue;
    if (r.length > 2 && r.startsWith('/') && r.endsWith('/')) {
      try {
        res.push(new RegExp(r.slice(1, -1), 'i'));
      } catch { /* skip invalid pattern */ }
    } else {
      subs.push(r.toLowerCase());
    }
  }
  return (cmd: string) => {
    const c = cmd.toLowerCase();
    return subs.some((s) => c.includes(s)) || res.some((re) => re.test(cmd));
  };
}

/**
 * fzf-style fuzzy match: every character of `needle` must appear in `hay` in
 * order (case-insensitive). Returns a score (higher is better) or -1 for no
 * match. Contiguous runs and matches at word boundaries score higher, so the
 * tightest, most word-aligned hits float to the top.
 */
export function fuzzyScore(hay: string, needle: string): number {
  const n = needle.trim().toLowerCase();
  if (!n) return 0;
  const h = hay.toLowerCase();
  let score = 0;
  let hi = 0;
  let prevMatch = -2;
  for (let ni = 0; ni < n.length; ni++) {
    const ch = n[ni];
    const found = h.indexOf(ch, hi);
    if (found < 0) return -1;
    score += 1;
    if (found === prevMatch + 1) score += 3; // adjacent to the previous hit
    const before = found > 0 ? hay[found - 1] : '';
    if (found === 0 || /[^A-Za-z0-9]/.test(before)) score += 2; // word boundary
    score -= Math.min(found - hi, 3); // small penalty for skipped chars
    prevMatch = found;
    hi = found + 1;
  }
  return score;
}

/** The positions in `hay` matched by `needle`, greedily, for highlighting. */
export function fuzzyIndices(hay: string, needle: string): number[] | null {
  const n = needle.trim().toLowerCase();
  if (!n) return [];
  const h = hay.toLowerCase();
  const out: number[] = [];
  let hi = 0;
  for (const ch of n) {
    const found = h.indexOf(ch, hi);
    if (found < 0) return null;
    out.push(found);
    hi = found + 1;
  }
  return out;
}

/** The first two words of a command: the default noise rule proposed for it. */
export function ruleFor(cmd: string): string {
  return cmd.trim().split(/\s+/).slice(0, 2).join(' ');
}

export type Column = { key: keyof Fields | 'num' | 'started' | 'ended'; header: string; short: string; optional?: boolean };

// Full column catalogue in team-sheet order. Optional columns (Tester, MITRE)
// are toggled so the export matches whichever sheet variant is being filled.
export const COLUMNS: Column[] = [
  { key: 'num', header: 'Number', short: '#' },
  { key: 'started', header: 'Started (CEST)', short: 'Started' },
  { key: 'ended', header: 'Ended (CEST)', short: 'Ended' },
  { key: 'measure', header: 'Measure', short: 'Measure' },
  { key: 'ie', header: 'External / Internal', short: 'Ext/Int' },
  { key: 'targets', header: 'Affected hosts / targets', short: 'Targets' },
  { key: 'source', header: 'Source (if via network)', short: 'Source' },
  { key: 'tester', header: 'Tester', short: 'Tester', optional: true },
  { key: 'mitre', header: 'MITRE TTP', short: 'MITRE', optional: true },
  { key: 'outcome', header: 'Outcome', short: 'Outcome' },
  { key: 'comment', header: 'Optional measure description / comment', short: 'Comment' },
];

export function activeColumns(hidden: string[]): Column[] {
  return COLUMNS.filter((c) => !c.optional || !hidden.includes(c.key));
}

/** Included items as TSV in time order, numbered 1..n, ready to paste into Excel. */
export function toTSV(items: Item[], cols: Column[], withHeader: boolean): string {
  const included = items.filter((it) => it.incl && !it.deleted).sort((a, b) => a.ts - b.ts);
  const clean = (s: string) => s.replace(/\t/g, ' ').replace(/\r?\n/g, ' ');
  const lines = included.map((it, i) =>
    cols.map((c) => clean(c.key === 'num' ? String(i + 1) : String(it[c.key] ?? ''))).join('\t'),
  );
  if (withHeader && lines.length) lines.unshift(cols.map((c) => c.header).join('\t'));
  return lines.join('\n');
}
