import assert from 'node:assert/strict';
import { test } from 'node:test';
import { toggleSelection, toggleVisible, matchPositions } from './selection.ts';

test('shift selection uses stable IDs in the current order and ignores hidden anchors', () => {
  const selected = new Set(['outside']);
  assert.deepEqual([...toggleSelection(['c', 'a', 'b'], selected, 'b', 'c', true)], ['outside', 'c', 'a', 'b']);
  assert.deepEqual([...toggleSelection(['c', 'b'], selected, 'b', 'a', true)], ['outside', 'b']);
  assert.deepEqual([...selected], ['outside']);
  assert.deepEqual([...toggleSelection(['a'], selected, 'missing', 'a', true)], ['outside']);
});

test('select-all toggles visible IDs without clearing other pages or filtered rows', () => {
  const selected = new Set(['hidden', 'a']);
  const all = toggleVisible(['a', 'b'], selected);
  assert.deepEqual([...all], ['hidden', 'a', 'b']);
  assert.deepEqual([...toggleVisible(['a', 'b'], all)], ['hidden']);
  assert.deepEqual([...toggleVisible([], selected)], ['hidden', 'a']);
});

test('literal output search reaches complete text beyond preview boundaries', () => {
  const text = 'start\n' + 'x'.repeat(100000) + '\nNEEDLE\nneedle';
  assert.deepEqual(matchPositions(text, ' needle '), [100007, 100014]);
  assert.deepEqual(matchPositions(text, '   '), []);
  assert.deepEqual(matchPositions('[[x]] [[x]]', '[[x]]'), [0, 6]);
});
