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
});

// Prefs are saved server-side ($XDG_CONFIG_HOME/pomdock/web-prefs.json) so they
// survive port changes; the theme is mirrored to localStorage to avoid a flash
// of the wrong theme before the first fetch returns.
const THEME_KEY = 'pomdock.theme';
try {
  const t = localStorage.getItem(THEME_KEY);
  if (t === 'light' || t === 'dark') app.prefs.theme = t;
} catch { /* storage unavailable */ }

let prefsTimer: ReturnType<typeof setTimeout> | undefined;
let prefsLoaded = false;

export function setPref<K extends keyof Prefs>(key: K, value: Prefs[K]) {
  app.prefs[key] = value;
  if (key === 'theme') {
    try { localStorage.setItem(THEME_KEY, String(value)); } catch { /* ignore */ }
  }
  if (!prefsLoaded) return; // never overwrite stored prefs with pre-load defaults
  clearTimeout(prefsTimer);
  prefsTimer = setTimeout(() => {
    api.savePrefs($state.snapshot(app.prefs)).catch((e) => toast(`saving preferences failed: ${e.message}`));
  }, 400);
}

export async function init() {
  try {
    const [meta, prefs] = await Promise.all([api.meta(), api.prefs()]);
    app.meta = meta;
    app.prefs = { ...app.prefs, ...(prefs ?? {}) };
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
  if (pick !== app.engagement) selectEngagement(pick);
}

export function selectEngagement(name: string) {
  app.engagement = name;
  if (route.view === 'tracker' || route.view === 'sessions') setParams({ name, id: '' });
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
    const t = document.createElement('textarea');
    t.value = text;
    document.body.appendChild(t);
    t.select();
    document.execCommand('copy');
    t.remove();
  }
}
