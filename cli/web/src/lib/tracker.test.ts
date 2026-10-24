// Run with `npm test` (node --test; Node >= 22.18 strips the types natively).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  activeColumns, buildItems, fuzzyScore, mergeItems, noiseMatcher, serialize, toTSV, type Defaults,
} from './tracker.ts';
import type { ReportRow } from './types.ts';

const d: Defaults = { tester: 'LS', source: '192.0.2.5' };

// By default atuin has no deliberate intent, so measure === command.
function row(id: string, ts: number, command: string, measure = command): ReportRow {
  return {
    id, ts, endTs: ts + 5, started: `s${ts}`, ended: `e${ts}`, measure, command,
    targets: '', tester: 'LS', exit: 0, noise: false,
  };
}

const rows = [row('a', 1, 'nmap -sV 10.0.0.1'), row('b', 2, 'ffuf -u http://x/FUZZ'), row('c', 3, 'ls')];
const byId = new Map(rows.map((r) => [r.id, r]));

test('untouched rows serialize to an empty document', () => {
  const items = buildItems(rows, null, d);
  assert.deepEqual(serialize(items, byId, d), { version: 1, rows: {}, merged: [] });
});

test('edits round-trip through the saved document', () => {
  const items = buildItems(rows, null, d);
  items[0].incl = true;
  items[0].measure = 'Port scan';
  items[1].deleted = true;
  const saved = serialize(items, byId, d);
  assert.deepEqual(saved.rows, { a: { incl: true, measure: 'Port scan' }, b: { deleted: true } });

  const again = buildItems(rows, saved, d);
  assert.equal(again[0].incl, true);
  assert.equal(again[0].measure, 'Port scan');
  assert.equal(again[0].origMeasure, 'nmap -sV 10.0.0.1');
  assert.equal(again[1].deleted, true);
  assert.deepEqual(serialize(again, byId, d), saved);
});

test('a plain command leaves the measure blank; an intent pre-fills it', () => {
  const items = buildItems([row('a', 1, 'nmap -sV 10.0.0.1'), row('b', 2, 'ffuf', 'Fuzz the web root')], null, d);
  assert.equal(items[0].measure, ''); // measure === command → not pre-filled
  assert.equal(items[0].comment, 'nmap -sV 10.0.0.1'); // comment always holds the full command
  assert.equal(items[1].measure, 'Fuzz the web root'); // pdsession intent kept as the measure
});

test('fuzzyScore: ordered subsequence matches, best hits score higher', () => {
  assert.equal(fuzzyScore('nmap -sV 10.0.0.1', 'xyz'), -1); // no match
  assert.ok(fuzzyScore('nmap -sV', 'nmap') > fuzzyScore('nmap -sV', 'nsv')); // contiguous beats scattered
  assert.ok(fuzzyScore('ffuf -u http://x', 'ffuf') >= 0);
  assert.equal(fuzzyScore('anything', ''), 0); // empty needle matches with no score
});

test('merged items absorb their rows and survive a reload', () => {
  const items = buildItems(rows, null, d);
  items[0].incl = items[1].incl = true;
  items[0].ie = 'External';
  items[0].measure = 'Port scan';
  items[1].measure = 'Fuzz web';
  const merged = mergeItems([items[1], items[0]], d);
  assert.deepEqual(merged.ids, ['a', 'b']);
  assert.equal(merged.measure, 'Port scan; Fuzz web');
  assert.equal(merged.ie, 'External');
  assert.equal(merged.ended, 'e2');

  const saved = serialize([items[2], merged], byId, d);
  const again = buildItems(rows, saved, d);
  assert.deepEqual(again.map((it) => it.key), ['c', merged.key]);
});

test('noise rules: substrings and /regex/, invalid regex ignored', () => {
  const m = noiseMatcher(['ifconfig.me', '/^git\\s/', '/(/']);
  assert.equal(m('curl IFCONFIG.ME'), true);
  assert.equal(m('git status'), true);
  assert.equal(m('legit status'), false);
});

test('TSV numbers included rows in time order and flattens newlines', () => {
  const items = buildItems(rows, null, d);
  items[2].incl = true;
  items[0].incl = true;
  items[0].comment = 'line1\nline2\tx';
  items[0].measure = 'Port scan';
  const cols = activeColumns(['mitre']).filter((c) => ['num', 'measure', 'comment'].includes(c.key));
  assert.equal(toTSV(items, cols, true), 'Number\tMeasure\tOptional measure description / comment\n1\tPort scan\tline1 line2 x\n2\t\tls');
});
