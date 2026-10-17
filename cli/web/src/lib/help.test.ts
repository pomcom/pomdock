import assert from 'node:assert/strict';
import { test } from 'node:test';
import { filterHelp, HELP } from './help.ts';

test('empty filter keeps every section', () => {
  assert.equal(filterHelp(HELP, '  ').length, HELP.length);
});

test('filter matches command or description, drops empty sections', () => {
  const got = filterHelp(HELP, 'RDP');
  assert.ok(got.length > 0);
  for (const s of got) for (const c of s.commands) assert.match(`${c.cmd} ${c.desc}`.toLowerCase(), /rdp/);
  assert.ok(got.every((s) => s.commands.length > 0));
});

test('commands are unique within a section', () => {
  for (const s of HELP) assert.equal(new Set(s.commands.map((c) => c.cmd)).size, s.commands.length, s.title);
});
