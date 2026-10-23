import assert from 'node:assert/strict';
import { test } from 'node:test';
import { outputPreview, sessionKey, sessionName, sessionState } from './sessions.ts';
import type { Session } from './types.ts';

const session: Session = { id: 'capture#0', engagement: 'test', source: 'script', title: 'Recorded session capture', tag: '', created: 0, date: '' };
test('names use saved labels, then tags, then titles, without raw ID fallbacks', () => {
  assert.equal(sessionName(session), 'Terminal recording');
  assert.equal(sessionName({ ...session, tag: 'recon' }), 'recon');
  assert.equal(sessionName({ ...session, title: 'Recorded session review' }), 'Recorded session review');
  assert.equal(sessionName({ ...session, source: 'imported-paste', title: 'Imported: terminal.log' }), 'Imported: terminal.log');
  assert.equal(sessionName(session, { [sessionKey(session)]: 'Review' }), 'Review');
  assert.notEqual(sessionKey(session), sessionKey({ ...session, engagement: 'other' }));
});
test('unclosed captures never claim confirmed liveness', () => {
  assert.equal(sessionState({ ...session, state: 'open' }), 'Open capture');
  assert.equal(sessionState({ ...session, state: 'ended' }), 'Ended');
  assert.equal(sessionState(session), 'Recorded');
});
test('output previews bound long lines and many lines without altering source', () => {
  const long = 'x'.repeat(1_000_000);
  assert.equal(outputPreview(long).length, 6000);
  assert.equal(long.length, 1_000_000);
  const lines = Array.from({ length: 100 }, (_, i) => `line ${i}`).join('\n');
  assert.equal(outputPreview(lines), lines.split('\n').slice(0, 12).join('\n'));
  assert.equal(outputPreview('  whitespace\n\tand tabs\n'), '  whitespace\n\tand tabs\n');
  assert.equal(outputPreview(''), '');
  assert.equal(outputPreview('123🙂more', 12, 4), '123');
});
