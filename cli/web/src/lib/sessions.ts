import type { Session } from './types';

export const PAGE_SIZE = 100;

export function sessionKey(s: Pick<Session, 'engagement' | 'id'>): string {
  return JSON.stringify([s.engagement, s.id]);
}

export function sessionName(s: Session, names: Record<string, string> = {}): string {
  const custom = names[sessionKey(s)];
  if (custom) return custom;
  if (s.tag) return s.tag;
  const generated = s.source === 'script' && s.title === `Recorded session ${s.id.split('#')[0]}`;
  if (s.title && !generated) return s.title;
  return s.source === 'script' ? 'Terminal recording' : 'Imported session';
}

export function sessionState(s: Session): string {
  if (s.state === 'open') return 'Open capture';
  if (s.state === 'ended') return 'Ended';
  if (s.state === 'imported' || s.source === 'imported-paste') return 'Imported';
  return 'Recorded';
}

export function sessionWhen(s: Session): string {
  if (s.created > 0) return new Date(s.created * 1000).toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
  return s.date || '';
}

// Slice before rendering: even a single multi-megabyte output stays inexpensive
// when collapsed. Copy always receives the original, complete string.
export function outputPreview(text: string, lines = 12, chars = 6000): string {
  let end = Math.min(text.length, chars);
  let from = 0;
  for (let n = 0; n < lines; n++) {
    const at = text.indexOf('\n', from);
    if (at < 0 || at >= end) break;
    if (n === lines - 1) end = at;
    from = at + 1;
  }
  if (end < text.length && /[\uD800-\uDBFF]/.test(text[end - 1] ?? '')) end--;
  return text.slice(0, end);
}
