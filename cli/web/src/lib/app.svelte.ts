import { api } from './api';
import { route, setParams } from './router.svelte';
import type { Engagement, Meta, Prefs } from './types';

// App-wide state shared by every view: server meta, the engagement list and
// current selection, persisted preferences, and the toast.

export const app = $state({
  meta: null as Meta | null,
  engagements: [] as Engagement[],
  engagement: '',
  prefs: {} as Prefs,
  error: '',
  engagementBusy: false,
});

// Prefs are saved server-side ($XDG_CONFIG_HOME/pomdock/web-prefs.json) so they
// survive port changes; the theme is mirrored to localStorage to avoid a flash
// of the wrong theme before the first fetch returns.
const THEME_KEY = 'pomdock.theme';
const LAYOUT_KEY = 'pomdock.layout';
const layoutKeys: (keyof Prefs)[] = ['theme', 'density', 'navWidth', 'navCollapsed', 'sessAsideWidth', 'sessAsideCollapsed', 'timelineWidth', 'replayTheme', 'trackerWidths', 'outputWrap'];
try {
  const t = localStorage.getItem(THEME_KEY);
  if (t === 'light' || t === 'dark' || t === 'system') app.prefs.theme = t;
  const local = JSON.parse(localStorage.getItem(LAYOUT_KEY) || '{}');
  for (const key of layoutKeys) if (local[key] !== undefined) Object.assign(app.prefs, { [key]: local[key] });
} catch { /* storage unavailable */ }

let prefsTimer: ReturnType<typeof setTimeout> | undefined;
let prefsLoaded = false;
let prefsDirty = false;
let prefsInFlight: Promise<void> | null = null;
let trackerFlush: (() => Promise<void>) | null = null;
export function registerTrackerFlush(flush: () => Promise<void>) { trackerFlush = flush; }
export async function flushTrackerEdits() { await trackerFlush?.(); }

export function setPref<K extends keyof Prefs>(key: K, value: Prefs[K]) {
  app.prefs[key] = value;
  if (key === 'theme') {
    try { localStorage.setItem(THEME_KEY, String(value)); } catch { /* ignore */ }
  }
  if (layoutKeys.includes(key)) {
    try { localStorage.setItem(LAYOUT_KEY, JSON.stringify(Object.fromEntries(layoutKeys.filter(k => app.prefs[k] !== undefined).map(k => [k, app.prefs[k]])))); } catch { /* ignore */ }
  }
  if (!prefsLoaded) return; // never overwrite stored prefs with pre-load defaults
  clearTimeout(prefsTimer);
  prefsDirty = true;
  prefsTimer = setTimeout(() => { void flushPreferences().catch((e) => toast(`saving preferences failed: ${e.message}`)); }, 400);
}

export async function flushPreferences() {
  clearTimeout(prefsTimer);
  while (prefsDirty || prefsInFlight) {
    if (prefsInFlight) { await prefsInFlight; continue; }
    prefsDirty = false;
    const task = api.savePrefs($state.snapshot(app.prefs)).then(() => {}).catch(e => { prefsDirty = true; throw e; });
    prefsInFlight = task;
    try { await task; } finally { if (prefsInFlight === task) prefsInFlight = null; }
  }
}

export async function init() {
  try {
    const [meta, prefs] = await Promise.all([api.meta(), api.prefs()]);
    app.meta = meta;
    const localLayout = Object.fromEntries(layoutKeys.filter(k => app.prefs[k] !== undefined).map(k => [k, app.prefs[k]]));
    app.prefs = { ...(prefs ?? {}), ...localLayout };
    prefsLoaded = true;
    await loadEngagements();
  } catch (e) {
    app.error = (e as Error).message;
  }
}

export async function loadEngagements(refresh = false) {
  app.engagements = await api.engagements(refresh);
  const names = app.engagements.map((e) => e.name);
  const pick = [route.params.name, app.engagement, app.meta?.engagement, app.prefs.engagement]
    .find((n) => n && names.includes(n)) ?? names[0] ?? '';
  if (pick !== app.engagement) selectEngagement(pick, route.params.name === pick);
}

export function selectEngagement(name: string, preserveSession = false) {
  app.engagement = name;
  if (route.view === 'tracker' || route.view === 'sessions') setParams({ name, ...(preserveSession ? {} : { id: '', archived: '' }) });
  if (name) setPref('engagement', name);
}

// --- toast ---------------------------------------------------------------------

export const toastState = $state({ message: '', undo: null as null | (() => void), visible: false });
let toastTimer: ReturnType<typeof setTimeout> | undefined;

export function toast(message: string, undo?: () => void) {
  toastState.message = message;
  toastState.undo = undo ?? null;
  toastState.visible = true;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (toastState.visible = false), undo ? 4000 : 1800);
}

export async function copyText(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const previous = document.activeElement as HTMLElement | null;
    const selection = window.getSelection();
    const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange()) : [];
    const t = document.createElement('textarea');
    t.value = text;
    t.style.cssText = 'position:fixed;left:-9999px;top:0';
    document.body.appendChild(t);
    try {
      t.select();
      if (!document.execCommand('copy')) throw new Error('Clipboard access failed. Select and copy the text manually.');
    } finally {
      t.remove();
      previous?.focus({ preventScroll: true });
      if (selection) { selection.removeAllRanges(); for (const range of ranges) selection.addRange(range); }
    }
  }
}
