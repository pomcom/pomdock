<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { api } from '../api';
  import { app, setPref, toast } from '../app.svelte';
  import { route, setParams } from '../router.svelte';
  import type { LogHit, Session, SessionEntry, SessionEntryChange } from '../types';
  import { PAGE_SIZE, sessionKey, sessionName, sessionState, sessionWhen, recordingPositions, lastRecording } from '../sessions';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import SessionOutput from './SessionOutput.svelte';
  import EngagementArchives from './EngagementArchives.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import { toggleSelection, toggleVisible, matchPositions } from '../selection';

  let sessions: Session[] = $state([]);
  let current: Session | null = $state(null);
  let tagFilter = $state('');
  let listFilter = $state('');
  let query = $state('');
  let hits: LogHit[] | null = $state(null);
  let error = $state('');
  let loadingList = $state(false);
  let opening = $state(false);
  let searching = $state(false);
  let expanded: Set<number> = $state(new Set());
  let archived = $state(false);
  let scope = $state<'session' | 'all'>('all');
  let findIn = $state('');
  let onlyMatches = $state(false);
  let cur = $state(0);
  let hitIndex = $state(0);
  let page = $state(0);
  let follow = $state(false);
  let scrollEl: HTMLDivElement | undefined = $state();
  let findEl: HTMLInputElement | undefined = $state();
  let details = $state(false);
  let renaming = $state(false);
  let newName = $state('');
  let viewport = $state(window.innerWidth);
  let resizing = $state(false);
  let showDeleted = $state(false);
  let showDeletedSessions = $state(false);
  let selectedEntries: Set<string> = $state(new Set());
  let selectedSessions: Set<string> = $state(new Set());
  let lastEntry: string | null = null;
  let mobileDetail = $state(false);
  let viewerHeading: HTMLHeadingElement | undefined = $state();
  let newOutput = $state(false);
  let raw = $state(false);
  let rawSource: { text: string; format: 'transcript' | 'session-json' } | null = $state(null);
  let rawLoading = $state(false);
  let pendingCurrent: Session | null = null;
  const narrow = $derived(viewport < 1000);
  let mutating = $state(false);
  let editing: { id: string; command: string; output: string } | null = $state(null);
  let searchVersion = 0;
  let openVersion = 0;
  let listVersion = 0;
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  const positions = recordingPositions;
  const names = $derived(app.prefs.sessionNames ?? {});
  const labels = $derived(new Map(sessions.map(s => [s.id, sessionName(s, names)])));
  const nameOf = (s: Session) => labels.get(s.id) ?? sessionName(s, names);
  const nameCounts = $derived.by(() => { const counts = new Map<string, number>(); for (const name of labels.values()) counts.set(name, (counts.get(name) ?? 0) + 1); return counts; });
  const searchIndex = $derived(new Map(sessions.map(s => [s.id, `${nameOf(s)} ${s.container ?? ''} ${s.date} ${sessionWhen(s)}`.toLowerCase()])));
  const collapsed = $derived(app.prefs.sessAsideCollapsed ?? false);
  const listVisible = $derived(narrow ? !mobileDetail : !collapsed);
  const asideWidth = $derived(Math.max(240, Math.min(420, app.prefs.sessAsideWidth ?? 288)));
  function listWhen(s: Session) {
    if (!s.created) return s.date;
    const generated = !s.displayName && !s.tag && s.title === `Recorded session ${s.id.split('#')[0]}`;
    return new Date(s.created * 1000).toLocaleString('en-GB', { timeZone: 'Europe/Berlin', day: 'numeric', month: 'short', ...(generated ? {} : { hour: '2-digit', minute: '2-digit' }) });
  }
  const tags = $derived([...new Set(sessions.map((s) => s.tag).filter(Boolean))]);
  const deletedSessions = $derived(sessions.filter(s => s.deleted).length);
  const cold = $derived(app.engagements.find(e => e.name === app.engagement)?.archived ?? false);
  const listed = $derived(sessions.filter((s) => (showDeletedSessions || !s.deleted) && (!tagFilter || s.tag === tagFilter) &&
    (searchIndex.get(s.id) ?? '').includes(listFilter.toLowerCase())));
  const entries: SessionEntry[] = $derived.by(() => current?.entries ?? []);
  const deletedEntries = $derived(entries.filter(e => e.deleted).length);
  const findTerm = $derived(findIn.trim().toLowerCase());
  type Occ = { e: number; field: 'cmd' | 'out'; pos: number };
  const occurrences = $derived.by<Occ[]>(() => {
    if (!findTerm) return [];
    const out: Occ[] = [];
    entries.forEach((en, e) => {
      if (en.deleted && !showDeleted) return;
      for (const [field, text] of [['cmd', en.command], ['out', en.output ?? '']] as const) {
        for (const pos of matchPositions(text, findTerm)) out.push({ e, field, pos });
      }
    });
    return out;
  });
  const matchSet = $derived(new Set(occurrences.map((o) => o.e)));
  const shownEntries = $derived((onlyMatches && findTerm ? [...matchSet] : entries.map((_, i) => i)).filter(i => showDeleted || !entries[i].deleted));
  const visibleEntries = $derived(shownEntries.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE));
  const curOcc = $derived(occurrences.length ? occurrences[cur % occurrences.length] : null);
  const pageCount = $derived(Math.ceil(shownEntries.length / PAGE_SIZE));

  function remember() {
    if (current && scrollEl && hits === null && !searching) positions.set(sessionKey(current), { top: scrollEl.scrollTop, page, expanded: new Set(expanded) });
  }

  function stopSearch() {
    clearTimeout(searchTimer);
    searchVersion++;
    searching = false;
  }

  onDestroy(() => { remember(); openVersion++; listVersion++; stopSearch(); });

  function setArchived(value: boolean) {
    if (archived === value) return;
    remember();
    openVersion++;
    opening = false;
    current = null; selectedSessions = new Set(); selectedEntries = new Set(); raw = false;
    archived = value;
    hits = null;
    query = findIn = tagFilter = listFilter = '';
    scope = 'all';
    setParams({ id: '', archived: value ? '1' : '' });
    stopSearch();
    loadList(app.engagement);
  }

  async function toggleArchive(s: Session) {
    const engagement = app.engagement;
    try {
      if (archived) await api.unarchiveSession(engagement, s.id);
      else await api.archiveSession(engagement, s.id);
      if (engagement !== app.engagement) return;
      // Tags share one capture file; refresh the whole list after a move.
      current = null;
      openVersion++;
      opening = false;
      setParams({ id: '' });
      await loadList(app.engagement);
      toast(archived ? 'Recording restored' : 'Recording archived');
    } catch (e) { toast((e as Error).message); }
  }

  async function loadList(name: string) {
    if (!name) return;
    const version = ++listVersion;
    const arch = archived;
    loadingList = true;
    error = '';
    try {
      const result = await api.sessions(name, arch);
      if (version !== listVersion || name !== app.engagement || arch !== archived) return;
      sessions = result;
      refreshEncryptedCount(name);
      const previous = route.params.id || lastRecording.get(JSON.stringify([name, arch]));
      if (previous && !current && result.some(s => s.id === previous && (!s.deleted || showDeletedSessions))) await open(previous);
    } catch (e) { if (version === listVersion) error = (e as Error).message; }
    finally { if (version === listVersion) loadingList = false; }
  }

  async function open(id: string, q = '', index = -1) {
    remember();
    const version = ++openVersion;
    const engagement = app.engagement;
    opening = true;
    try {
      const result = await api.session(engagement, id, archived);
      if (version !== openVersion || engagement !== app.engagement) return;
      current = result; pendingCurrent = null; newOutput = false; raw = false; rawSource = null; mobileDetail = true;
      selectedEntries = new Set(); editing = null; showDeleted = false; lastEntry = null;
      error = '';
      hits = null;
      stopSearch();
      findIn = q;
      query = '';
      scope = 'session';
      onlyMatches = false;
      details = false;
      const saved = positions.get(sessionKey(result));
      expanded = saved?.expanded ?? new Set();
      follow = result.state === 'open' && !saved;
      const visible = (result.entries ?? []).filter(e => !e.deleted);
      page = saved?.page ?? (follow ? Math.max(0, Math.ceil(visible.length / PAGE_SIZE) - 1) : 0);
      if (index >= 0) { page = Math.floor((result.entries ?? []).slice(0, index).filter(e => !e.deleted).length / PAGE_SIZE); expanded = new Set([...expanded, index]); follow = false; }
      lastRecording.set(JSON.stringify([engagement, archived]), id);
      setParams({ id, archived: archived ? '1' : '' });
      await tick();
      if (scrollEl) scrollEl.scrollTop = follow ? scrollEl.scrollHeight : saved?.top ?? 0;
      if (index >= 0) document.getElementById(`entry-${index}`)?.scrollIntoView({ block: 'center' });
      if (narrow) viewerHeading?.focus({ preventScroll: true });
    } catch (e) { if (version === openVersion) error = (e as Error).message; }
    finally { if (version === openVersion) opening = false; }
  }

  async function refreshCurrent() {
    if (!current || opening || mutating || editing || raw) return;
    const id = current.id, engagement = app.engagement, arch = archived, version = openVersion;
    try {
      const result = await api.session(engagement, id, arch);
      if (version !== openVersion || current?.id !== id || engagement !== app.engagement || arch !== archived) return;
      const changed = (result.entries?.length !== current.entries?.length) || (result.entries ?? []).some((entry, i) => entry.output !== current?.entries?.[i]?.output || entry.command !== current?.entries?.[i]?.command);
      if (!changed && result.state === current.state) { error = ''; return; }
      if (!follow && changed) newOutput = true;
      const selection = window.getSelection();
      if (selection && !selection.isCollapsed && scrollEl?.contains(selection.anchorNode)) { pendingCurrent = result; follow = false; newOutput = true; return; }
      const top = scrollEl?.scrollTop ?? 0;
      current = result; pendingCurrent = null; error = '';
      if (!follow) { await tick(); if (scrollEl) scrollEl.scrollTop = top; }
      sessions = sessions.map((s) => s.id === id ? { ...result, entries: undefined } : s);
      if (follow && !findTerm) {
        page = Math.max(0, Math.ceil((result.entries ?? []).filter(e => showDeleted || !e.deleted).length / PAGE_SIZE) - 1);
        await tick();
        if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
      }
    } catch (e) { if (version === openVersion && engagement === app.engagement) error = (e as Error).message; }
  }

  $effect(() => {
    const engagement = app.engagement;
    untrack(() => {
      remember(); openVersion++; listVersion++; stopSearch();
      current = null; sessions = []; hits = null;
      opening = loadingList = follow = false; raw = false; rawSource = null; pendingCurrent = null; newOutput = false; mobileDetail = false;
      page = 0;
      selectedEntries = new Set(); selectedSessions = new Set(); editing = null;
      showDeleted = showDeletedSessions = false;
      encryptedCount = 0;
      query = findIn = tagFilter = listFilter = '';
      onlyMatches = false; scope = 'all';
      archived = route.params.archived === '1';
      if (engagement) loadList(engagement);
    });
  });

  $effect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible' && current?.state === 'open' && !archived) refreshCurrent();
    }, 3000);
    return () => { clearInterval(timer); clearTimeout(searchTimer); };
  });

  async function goto(delta: number) {
    if (!occurrences.length) return;
    follow = false;
    cur = (cur + delta + occurrences.length) % occurrences.length;
    const target = occurrences[cur];
    page = Math.floor(shownEntries.indexOf(target.e) / PAGE_SIZE);
    await tick();
    document.getElementById('find-cur')?.scrollIntoView({ block: 'center' });
  }

  $effect(() => {
    findTerm;
    untrack(() => { cur = 0; if (findTerm) goto(0); });
  });

  function setScope(value: 'session' | 'all') {
    if (value === 'session' && !current) return;
    stopSearch();
    follow = false;
    scope = value;
    hits = null; query = findIn = '';
    tick().then(() => findEl?.focus());
  }

  function onsearch() {
    remember();
    follow = false;
    stopSearch();
    const q = query.trim(), engagement = app.engagement, version = searchVersion;
    hits = null;
    if (!q) return;
    searching = true;
    searchTimer = setTimeout(async () => {
      try {
        const result = await api.logSearch(engagement, q);
        if (version === searchVersion && engagement === app.engagement && scope === 'all') { hits = result; hitIndex = 0; }
      } catch (e) { if (version === searchVersion) error = (e as Error).message; }
      finally { if (version === searchVersion) searching = false; }
    }, 180);
  }

  async function openHit(h: LogHit) {
    const q = query.trim();
    if (archived) { archived = false; await loadList(app.engagement); }
    await open(h.sessionId, q, h.index);
  }

  async function gotoHit(delta: number) {
    if (!hits?.length) return;
    hitIndex = (hitIndex + delta + hits.length) % hits.length;
    await tick();
    document.getElementById(`hit-${hitIndex}`)?.focus();
  }

  function toggle(i: number) {
    const next = new Set(expanded);
    next.has(i) ? next.delete(i) : next.add(i);
    expanded = next;
  }

  async function changePage(value: number) {
    follow = false;
    page = value;
    await tick();
    if (scrollEl) scrollEl.scrollTop = 0;
  }

  async function resumeFollow() {
    if (pendingCurrent) { current = pendingCurrent; pendingCurrent = null; }
    findIn = ''; onlyMatches = false; follow = true; newOutput = false;
    page = Math.max(0, pageCount - 1);
    await tick();
    if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
  }

  function observeViewport(node: HTMLElement) {
    const observer = new ResizeObserver(() => { if (follow && !findTerm && !raw) tick().then(() => { if (follow) node.scrollTop = node.scrollHeight; }); });
    observer.observe(node); return { destroy: () => observer.disconnect() };
  }

  function onscroll() {
    if (scrollEl && follow && scrollEl.scrollHeight - scrollEl.clientHeight - scrollEl.scrollTop > 48) follow = false;
  }

  async function rename() {
    if (!current) return;
    mutating = true;
    const name = app.engagement, id = current.id;
    try {
      const result = await api.editSession(name, id, { displayName: newName.trim() });
      if (name === app.engagement && current?.id === id) { current = result; sessions = sessions.map(s => s.id === id ? { ...s, displayName: result.displayName } : s); renaming = false; }
    } catch (e) { error = (e as Error).message; }
    finally { mutating = false; }
  }

  function selectEntry(i: number, shift: boolean) {
    follow = false;
    selectedEntries = toggleSelection(shownEntries.map(index => entries[index].id), selectedEntries, entries[i].id, lastEntry, shift);
    lastEntry = entries[i].id;
  }

  function selectAllEntries() {
    selectedEntries = toggleVisible(shownEntries.map(i => entries[i].id), selectedEntries); follow = false;
  }

  async function saveEntries(changes: SessionEntryChange[]) {
    if (!current || mutating) return false;
    const name = app.engagement, id = current.id;
    mutating = true; follow = false; error = '';
    try {
      const result = await api.editSession(name, id, { entries: changes });
      if (name === app.engagement && current?.id === id) { const top = scrollEl?.scrollTop ?? 0; current = result; selectedEntries = new Set(); page = Math.max(0, Math.min(page, pageCount - 1)); await tick(); if (scrollEl) scrollEl.scrollTop = top; }
      return true;
    } catch (e) { error = (e as Error).message; return false; }
    finally { mutating = false; }
  }

  async function deleteEntries(ids: string[], deleted: boolean) {
    const name = app.engagement, session = current?.id;
    if (await saveEntries(ids.map(id => ({ id, deleted })))) {
      toast(deleted ? `${ids.length} deleted · originals kept` : `${ids.length} restored`, deleted ? () => { if (name === app.engagement && session === current?.id) void saveEntries(ids.map(id => ({ id, deleted: false }))); } : undefined);
    }
  }

  async function saveEdit(reset = false) {
    if (!editing) return;
    if (await saveEntries([reset ? { id: editing.id, reset: true } : { id: editing.id, command: editing.command, output: editing.output }])) editing = null;
  }

  async function deleteRecordings(deleted: boolean) {
    const name = app.engagement, ids = [...selectedSessions];
    if (!ids.length || mutating) return;
    mutating = true; error = '';
    try {
      await api.deleteSessions(name, ids, deleted);
      if (name !== app.engagement) return;
      if (current && ids.includes(current.id) && deleted) { current = null; openVersion++; setParams({ id: '' }); }
      selectedSessions = new Set();
      await loadList(name);
    } catch (e) { error = (e as Error).message; }
    finally { mutating = false; }
  }

  async function archiveChanged() {
    current = null; selectedEntries = new Set(); selectedSessions = new Set();
    archived = false; showDeletedSessions = false;
    openVersion++; setParams({ id: '', archived: '' });
    await loadList(app.engagement);
  }

  function startResize(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    resizing = true;
  }
  function resize(e: PointerEvent) {
    if (!resizing) return;
    const rect = (e.currentTarget as HTMLElement).parentElement!.getBoundingClientRect();
    setPref('sessAsideWidth', Math.max(240, Math.min(420, e.clientX - rect.left)));
  }
  function resizeKey(e: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    setPref('sessAsideWidth', e.key === 'Home' ? 240 : e.key === 'End' ? 420 : Math.max(240, Math.min(420, asideWidth + (e.key === 'ArrowLeft' ? -10 : 10))));
  }
  const when = (e: { date: string; time: string }) => [e.date, e.time].filter(Boolean).join(' · ');
  function findKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); scope === 'session' ? goto(e.shiftKey ? -1 : 1) : gotoHit(e.shiftKey ? -1 : 1); }
    if (e.key === 'Escape') { stopSearch(); findIn = query = ''; hits = null; }
  }
  function browse() { mobileDetail = false; setPref('sessAsideCollapsed', false); tick().then(() => document.querySelector<HTMLButtonElement>('.sess.active, .sess')?.focus()); }
  function collapseList() { if (narrow) mobileDetail = true; else setPref('sessAsideCollapsed', true); }
  function focusSearch() { if (narrow && listVisible) document.querySelector<HTMLInputElement>('[aria-label="Filter sessions by name, container or date"]')?.focus(); else findEl?.focus(); }
  function shortcuts(e: KeyboardEvent) {
    const el = document.activeElement;
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || (el instanceof HTMLElement && (el.isContentEditable || el.closest('[role=dialog], [role=menu], .xterm, [data-terminal]'))) || el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) return;
    if (e.key === '/') { e.preventDefault(); focusSearch(); }
  }
  $effect(() => { window.addEventListener('pomdock:search', focusSearch); window.addEventListener('pomdock:browse', browse); return () => { window.removeEventListener('pomdock:search', focusSearch); window.removeEventListener('pomdock:browse', browse); }; });
  async function toggleRaw() {
    raw = !raw; follow = false;
    if (!raw || !current) return;
    const id = current.id, name = app.engagement;
    rawLoading = true; rawSource = null;
    try { const source = await api.sessionSource(name, id, archived); if (name === app.engagement && current?.id === id) rawSource = source; }
    catch (e) { error = (e as Error).message; }
    finally { rawLoading = false; }
  }
  function listKeys(e: KeyboardEvent) {
    if (!(e.target instanceof HTMLElement) || !e.target.matches('.sess') || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    const rows = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('.sess')], index = rows.indexOf(e.target as HTMLButtonElement);
    rows[e.key === 'Home' ? 0 : e.key === 'End' ? rows.length - 1 : Math.max(0, Math.min(rows.length - 1, index + (e.key === 'ArrowDown' ? 1 : -1)))]?.focus();
  }
  // --- encrypt (cold-store) ----------------------------------------------------
  let encryptedCount = $state(0);

  async function refreshEncryptedCount(name: string) {
    try {
      const result = await api.encryptedArchives(name);
      if (name === app.engagement) encryptedCount = result.length;
    } catch {
      if (name === app.engagement) encryptedCount = 0;
    }
  }

</script>

<svelte:window bind:innerWidth={viewport} onkeydown={shortcuts} />
<div class="sessions" class:list-hidden={!listVisible} class:narrow class:resizing style:--aside-width={`${asideWidth}px`}>
  {#if listVisible}
    <aside aria-label="Session list">
      <div class="head"><strong>Recordings <span class="muted">{sessions.length}</span></strong><span class="grow"></span>
        <button class="sm ghost icon" title="Refresh recordings" aria-label="Refresh recordings" onclick={() => loadList(app.engagement)}><Icon name="refresh" size={14} /></button>
        <button class="sm ghost icon" title="Collapse session list" aria-label="Collapse session list" aria-expanded="true" onclick={collapseList}><Icon name="panel" size={14} /></button>
      </div>
      <div class="scopes" aria-label="Recording collection"><button class:on={!archived} aria-pressed={!archived} onclick={() => setArchived(false)}>Recordings</button><button class:on={archived} aria-pressed={archived} onclick={() => setArchived(true)}>Archived</button></div>
      <div class="list-filter"><Icon name="search" size={14} /><input aria-label="Filter sessions by name, container or date" bind:value={listFilter} placeholder="Filter sessions…" /></div>
      {#if selectedSessions.size}<div class="selection-bar"><span>{selectedSessions.size} selected</span><button class="sm danger" disabled={mutating} onclick={() => deleteRecordings(true)}>Delete</button><button class="sm" disabled={mutating} onclick={() => deleteRecordings(false)}>Restore</button><button class="sm ghost" onclick={() => selectedSessions = new Set()}>Clear</button></div>{/if}
      {#if listed.length}<label class="list-select"><input type="checkbox" aria-label="Select all matching recordings" checked={listed.length > 0 && listed.every(s => selectedSessions.has(s.id))} onchange={(e) => { const next = new Set(selectedSessions); for (const s of listed) { if (e.currentTarget.checked) next.add(s.id); else next.delete(s.id); } selectedSessions = next; }} /> Select all</label>{/if}
      {#if tags.length}<label class="tag-filter">Tag <select aria-label="Filter by tag" bind:value={tagFilter}><option value="">All tags</option>{#each tags as t}<option value={t}>{t}</option>{/each}</select></label>{/if}
      {#if error && narrow}<div class="error" role="alert">{error}<button class="sm" onclick={() => loadList(app.engagement)}>Retry</button></div>{/if}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="list" aria-busy={loadingList} onkeydown={listKeys}>
        {#each listed as s (s.id)}
          <div class="session-row" class:deleted={s.deleted} class:checked={selectedSessions.has(s.id)}>
          <input type="checkbox" aria-label={`Select recording ${nameOf(s)}`} checked={selectedSessions.has(s.id)} disabled={mutating} onchange={(e) => { const next = new Set(selectedSessions); if (e.currentTarget.checked) next.add(s.id); else next.delete(s.id); selectedSessions = next; }} />
          <button class="sess" title={`${nameOf(s)} · ${s.id}`} class:active={current?.id === s.id} aria-current={current?.id === s.id ? 'true' : undefined} disabled={mutating} onclick={() => open(s.id)}>
            <span class="session-name">{nameOf(s)}</span>
            <span class="session-meta identity">{#if s.container}<span class="container">{s.container}</span>{/if}<span>{listWhen(s)}{#if listWhen(s)}{' · '}{/if}{sessionState(s)}</span>{#if (nameCounts.get(nameOf(s)) ?? 0) > 1}<span class="short-id">…{s.id.slice(-8)}</span>{/if}</span>
          </button>
          </div>
        {:else}<div class="hintbox">{#if loadingList}Loading recordings…{:else if error}Could not load recordings.{:else if !app.engagement}Choose an engagement to browse its recordings.{:else if cold}Engagement archived. Restore it below.{:else if listFilter || tagFilter}No sessions match these filters.{:else if deletedSessions}Enable Show deleted to restore recordings.{:else if archived}No archived recordings.{:else}No recordings yet. Open a terminal from <a href="#/containers">Containers</a> to begin.<br /><br />Label a section with <code>pdsession recon</code>, or import with <code>pomdock web import file.log -n {app.engagement}</code>.{/if}</div>{/each}
      </div>
      {#if deletedSessions}<label class="list-select"><input type="checkbox" bind:checked={showDeletedSessions} /> Show deleted ({deletedSessions})</label>{/if}
      {#if encryptedCount}<div class="enc-note">{encryptedCount} legacy encrypted captures. Restore via <code>pomdock web session-restore</code>.</div>{/if}
      <div class="list-foot">{archived ? 'Archived captures' : 'Local recordings & imported transcripts'}</div>
      <EngagementArchives name={app.engagement} onchanged={archiveChanged} disabled={mutating || !!editing} />
    </aside>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
    <div class="resize-handle" role="separator" aria-label="Resize session list" aria-orientation="vertical" aria-valuemin={240} aria-valuemax={420} aria-valuenow={asideWidth} tabindex="0" onpointerdown={startResize} onpointermove={resize} onpointerup={() => resizing = false} onlostpointercapture={() => resizing = false} onkeydown={resizeKey}></div>
  {/if}
  <section class="viewer" class:mobile-hidden={narrow && listVisible} aria-label="Session viewer" aria-busy={opening}>
    <div class="viewer-head">
      {#if !listVisible}<button class="sm ghost browse" title="Browse recordings" aria-label="Browse recordings" aria-expanded="false" onclick={browse}><Icon name={narrow ? "arrow" : "panel"} /><span>{narrow ? "Recordings" : "Browse"}</span></button>{/if}
      <div class="heading"><h1 bind:this={viewerHeading} tabindex="-1">{current ? nameOf(current) : 'Recordings'}</h1><div class="session-meta">
        {#if current}<span class="state"><span class="status-dot" class:live={current.state === 'open'}></span>{sessionState(current)}</span>
          {#if current.container}<span class="container">{current.container}</span>{/if}
          {#if sessionWhen(current)}<span>Recorded {sessionWhen(current)}</span>{/if}
          {#if current.ended}<span>Ended {new Date(current.ended * 1000).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' })}</span>{/if}<span>{entries.length} executions</span>
        {:else}Commands and output from your local terminal recordings{/if}
      </div></div>
      {#if current}<div class="header-actions"><button class="sm ghost icon" title="Refresh recording" aria-label="Refresh recording" onclick={refreshCurrent}><Icon name="refresh" /></button><button class="sm ghost" aria-expanded={details} onclick={() => details = !details}>Details</button><ActionMenu label="Recording actions"><button role="menuitem" onclick={() => { newName = current?.displayName ?? names[sessionKey(current!)] ?? ''; renaming = true; }}>Rename recording</button><button role="menuitem" onclick={toggleRaw}>{raw ? 'View executions' : 'View original source'}</button><button role="menuitem" onclick={() => current && toggleArchive(current)}>{archived ? 'Restore recording' : 'Archive entire capture'}</button></ActionMenu></div>{/if}
    </div>
    {#if current && details}<div class="details">
      <dl><dt>Recording ID</dt><dd><code>{current.id}</code><CopyButton text={current.id} title="Copy recording ID" /></dd><dt>Timezone</dt><dd>Europe/Berlin</dd><dt>Started</dt><dd>{current.created ? new Date(current.created * 1000).toLocaleString('en-GB', { timeZone: 'Europe/Berlin', timeZoneName: 'short' }) : 'Not recorded'}{#if current.created}<CopyButton text={new Date(current.created * 1000).toISOString()} title="Copy start timestamp (ISO 8601)" />{/if}</dd>{#if current.ended}<dt>Ended</dt><dd>{new Date(current.ended * 1000).toLocaleString('en-GB', { timeZone: 'Europe/Berlin', timeZoneName: 'short' })}<CopyButton text={new Date(current.ended * 1000).toISOString()} title="Copy end timestamp (ISO 8601)" /></dd>{/if}<dt>Source</dt><dd>{current.source === 'script' ? 'Terminal capture' : 'Imported transcript'}</dd><dt>Container</dt><dd>{current.container || 'Not recorded in this capture'}</dd><dt>Engagement</dt><dd>{current.engagement}</dd></dl>
      {#if current.state === 'open'}<p>An open capture has no end marker. Its terminal may still be running; interrupted captures can also remain open.</p>{/if}
      <p>Command timing is shown when recorded. Exit status and duration are unavailable in this capture format.</p>
      <div class="recording-actions"><span>Entire capture, including tagged sections</span><button class="sm" onclick={() => current && toggleArchive(current)}>{archived ? 'Restore recording' : 'Archive recording'}</button></div>
    </div>{/if}
    {#if !raw}<div class="searchbar">
      <label class="scope-label"><span class="sr-only">Search scope</span><select aria-label="Search scope" value={scope} onchange={(e) => setScope(e.currentTarget.value as 'session' | 'all')}><option value="session" disabled={!current}>This recording</option><option value="all">This engagement</option></select></label>
      <div class="searchwrap"><Icon name="search" size={14} />
        {#if scope === 'all'}<input bind:this={findEl} aria-label="Search all non-archived recordings in this engagement" bind:value={query} oninput={onsearch} onkeydown={findKeydown} placeholder="Commands & output…" />{:else}<input bind:this={findEl} aria-label="Search commands and output in current session" bind:value={findIn} onkeydown={findKeydown} placeholder="Commands & output…" />{/if}
        {#if (scope === 'session' && findTerm) || (scope === 'all' && query.trim())}
          <span class="counter" role="status">{scope === 'session' ? (occurrences.length ? `${(cur % occurrences.length) + 1} / ${occurrences.length}` : '0 matches') : searching ? 'Searching…' : hits?.length ? `${hitIndex + 1} / ${hits.length}` : '0 matches'}</span>
          <button class="navbtn" title="Previous match (Shift+Enter)" aria-label="Previous match" disabled={scope === 'session' ? !occurrences.length : !hits?.length} onclick={() => scope === 'session' ? goto(-1) : gotoHit(-1)}>↑</button><button class="navbtn" title="Next match (Enter)" aria-label="Next match" disabled={scope === 'session' ? !occurrences.length : !hits?.length} onclick={() => scope === 'session' ? goto(1) : gotoHit(1)}>↓</button><button class="navbtn" title="Clear search" aria-label="Clear search" onclick={() => { stopSearch(); findIn = query = ''; hits = null; }}>×</button>
        {/if}
      </div>
      {#if scope === 'session' && findTerm}<label class="fonly"><input type="checkbox" bind:checked={onlyMatches} onchange={() => { page = 0; tick().then(() => goto(0)); }} /> Matches only</label>{/if}
    </div>
    {#if scope === 'all'}<div class="scope-note">This engagement · archived recordings excluded</div>{/if}{/if}
    {#if current && hits === null && !searching && !raw}
      <div class="selection-bar">
        <label><input type="checkbox" aria-label="Select all matching executions" checked={shownEntries.length > 0 && shownEntries.every(i => selectedEntries.has(entries[i].id))} disabled={!shownEntries.length || mutating} onchange={selectAllEntries} /> Select all</label>
        {#if selectedEntries.size}<span>{selectedEntries.size} selected</span><button class="sm danger" disabled={mutating} onclick={() => deleteEntries([...selectedEntries], true)}>Delete selected</button>{#if showDeleted}<button class="sm" disabled={mutating} onclick={() => deleteEntries([...selectedEntries], false)}>Restore selected</button>{/if}<button class="sm ghost" onclick={() => selectedEntries = new Set()}>Clear</button>{/if}
        <span class="grow"></span>
        {#if deletedEntries}<label><input type="checkbox" bind:checked={showDeleted} onchange={() => { page = 0; selectedEntries = new Set(); }} /> Show deleted ({deletedEntries})</label>{/if}
      </div>
    {/if}
    {#if error}<div class="error" role="alert">{error}<button class="sm ghost" onclick={() => { error = ''; current ? refreshCurrent() : loadList(app.engagement); }}>Retry</button></div>{/if}
    {#if opening}<div class="loading" role="status">Opening recording…</div>{/if}
    <!-- Focusable so keyboard users can scroll the output pane directly. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="scroll" role="region" aria-label="Recorded commands and output" tabindex="0" bind:this={scrollEl} use:observeViewport {onscroll} onpointerdown={(e) => { if ((e.target as HTMLElement).closest('pre, .output-panel')) follow = false; }}>
      {#if raw && current}
        <div class="source-heading"><strong>{rawSource?.format === 'session-json' ? 'Original parsed data' : 'Original transcript'}</strong><button class="sm" onclick={toggleRaw}>View executions</button></div>
        {#if rawLoading}<div class="empty" role="status">Loading original source…</div>{:else if rawSource}<p class="source-note">{rawSource.format === 'session-json' ? 'This import retained parsed JSON. Its raw transcript is unavailable.' : 'Original capture, including tagged sections. Control escapes are displayed as ␛; copying preserves the stored text.'}</p><CopyButton text={rawSource.text} title="Copy original source" label="Copy original source" /><pre class="source-text">{rawSource.text.replaceAll('\x1b', '␛')}</pre>{/if}
      {:else if searching}<div class="empty" role="status">Searching recordings…</div>
      {:else if hits !== null}
        <div class="results-meta">{hits.length} matching executions for “{query.trim()}”</div>
        {#each hits as h, hi (`${h.sessionId}:${h.index}`)}<button class="entry hit" class:current-hit={hitIndex === hi} id="hit-{hi}" onclick={() => openHit(h)} onfocus={() => hitIndex = hi}>
          <span class="meta">{nameOf(sessions.find((s) => s.id === h.sessionId) ?? { id: h.sessionId, title: h.title, source: 'script', engagement: app.engagement, tag: '', date: h.date, created: 0 })}{#if when(h)} · {when(h)}{/if} · {h.inOutput ? 'Output match' : 'Command match'}</span><span class="hitcmd"><Highlight text={h.command} q={query} /></span>{#if h.snippet}<span class="hitsnip"><Highlight text={h.snippet} q={query} /></span>{/if}
        </button>{:else}<div class="empty">No matches for “{query.trim()}”. Try a shorter command, path, or output fragment.</div>{/each}
      {:else if current}
        {#if findTerm && !occurrences.length}<div class="empty" role="status">No commands or output match “{findIn.trim()}” in this session.</div>{/if}
        {#if !shownEntries.length && !findTerm}<div class="empty">{deletedEntries ? 'All executions are deleted. Enable Show deleted to restore them.' : 'This session has no recorded executions yet.'}</div>{/if}
        {#each visibleEntries as i (`${current.id}:${i}`)}
          {@const e = entries[i]}{@const output = e.output ?? ''}{@const forcedOpen = curOcc?.e === i && curOcc?.field === 'out'}
          <article class="entry execution" class:deleted={e.deleted} class:selected={selectedEntries.has(e.id)} id="entry-{i}" aria-label={`Execution ${i + 1}`}>
            <div class="execution-head">
              <input class="entry-select" type="checkbox" aria-label={`Select execution ${i + 1}`} checked={selectedEntries.has(e.id)} disabled={mutating} onclick={(event) => selectEntry(i, event.shiftKey)} />
              <div class="execution-meta">{#if when(e)}<time class="when">{when(e)}</time>{:else}<span>Time not recorded</span>{/if}{#if e.cwd}<span class="cwd" title={e.cwd}>{e.cwd}</span>{/if}</div>
              <div class="entry-actions">{#if e.edited}<span class="edited">Edited</span>{/if}<button class="sm ghost icon" aria-label={`Edit execution ${i + 1}`} title="Edit execution" disabled={mutating} onclick={() => { follow = false; editing = { id: e.id, command: e.command, output }; error = ''; }}><Icon name="edit" /></button><button class="sm ghost icon" title={e.deleted ? 'Restore execution' : 'Delete execution'} aria-label={`${e.deleted ? 'Restore' : 'Delete'} execution ${i + 1}`} disabled={mutating} onclick={() => deleteEntries([e.id], !e.deleted)}><Icon name={e.deleted ? 'restore' : 'trash'} /></button></div>
            </div>
            <div class="command-line"><span class="prompt" aria-hidden="true">❯</span><pre class="command"><Highlight text={e.command} q={findIn} curStart={curOcc?.e === i && curOcc.field === 'cmd' ? curOcc.pos : -1} curId="find-cur" /></pre><CopyButton text={e.command} title="Copy command" /></div>
            {#if output.length}
              <SessionOutput text={output} expanded={expanded.has(i)} onToggle={() => toggle(i)} onFind={() => follow = false} query={findIn} currentMatch={forcedOpen ? curOcc.pos : -1} {forcedOpen} />
            {:else}<div class="noout">{e.output == null ? 'Output unavailable in this recording' : 'No output captured'}</div>{/if}
          </article>
        {/each}
      {:else}<div class="empty welcome"><Icon name="sessions" size={28} /><strong>{cold ? 'Engagement archived' : 'Choose a recording'}</strong><span>{cold ? 'Restore an encrypted package from Archives in the recording list.' : 'Review commands and output, or search this engagement’s recordings.'}</span><button class="sm" onclick={browse}>Browse recordings</button>{#if !cold}<a href="#/shells">View open terminals →</a>{/if}</div>{/if}
    </div>
    {#if current && hits === null && !searching && !raw}<div class="viewer-foot"><span class="range">{shownEntries.length ? `${page * PAGE_SIZE + 1}–${Math.min((page + 1) * PAGE_SIZE, shownEntries.length)} of ${shownEntries.length}` : '0'} executions</span>{#if pageCount > 1}<button class="sm ghost" disabled={page === 0} onclick={() => changePage(page - 1)}>Previous</button><button class="sm ghost" disabled={page >= pageCount - 1} onclick={() => changePage(page + 1)}>Next</button>{/if}<span class="grow"></span>{#if current.state === 'open' && !archived}<span class="follow-state">{follow ? 'Following capture' : newOutput ? 'New output · paused' : 'Follow paused'}</span><button class="sm" class:following={follow} aria-pressed={follow} onclick={() => follow ? follow = false : resumeFollow()}>{follow ? 'Pause following' : 'Resume following ↓'}</button>{/if}</div>{/if}
  </section>
</div>
{#if renaming && current}<Modal title="Rename session" busy={mutating} onclose={() => { if (!mutating) renaming = false; }}><form onsubmit={(e) => { e.preventDefault(); rename(); }}><label class="rename-label">Session name <input bind:value={newName} placeholder={sessionName(current)} maxlength="160" disabled={mutating} /></label>{#if error}<p class="edit-error" role="alert">{error}</p>{/if}<div class="modal-actions"><button type="submit" class="accent" disabled={mutating}>Save name</button></div></form></Modal>{/if}
{#if editing}<Modal title="Edit execution" busy={mutating} onclose={() => { if (!mutating) editing = null; }}><form onsubmit={(e) => { e.preventDefault(); saveEdit(); }}><label class="rename-label">Command<textarea class="edit-text" rows="3" bind:value={editing.command} disabled={mutating}></textarea></label><label class="rename-label">Output<textarea class="edit-text" rows="12" bind:value={editing.output} disabled={mutating}></textarea></label>{#if error}<p class="edit-error" role="alert">{error}</p>{/if}<div class="modal-actions"><button type="button" disabled={mutating} onclick={() => saveEdit(true)}>Restore original</button><button type="submit" class="accent" disabled={mutating}>Save changes</button></div></form></Modal>{/if}


<style>
  .sessions { display: grid; grid-template-columns: var(--aside-width) 6px minmax(0, 1fr); height: 100%; min-height: 0; }
  .sessions.list-hidden { grid-template-columns: minmax(0, 1fr); }
  aside { background: var(--pd-sidebar); display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .head { display: flex; align-items: center; gap: 4px; padding: 12px 12px 8px; }.head strong { font-size: .875rem; font-weight: 600; }.muted { color: var(--pd-text-muted); margin-left: 4px; font-weight: 400; }
  .icon { display: inline-flex; align-items: center; justify-content: center; min-width: 32px; padding: 4px; }
  .scopes { display: flex; flex-wrap: wrap; gap: 4px; padding: 0 12px 8px; }.scopes button { flex: 1; min-width: min-content; border-color: transparent; background: none; color: var(--pd-text-muted); }.scopes button.on { background: var(--pd-surface); border-color: var(--pd-control-border); color: var(--pd-text); }
  .list-filter { display: flex; align-items: center; gap: 8px; padding-left: 10px; margin: 0 12px 8px; background: var(--pd-surface); border: 1px solid var(--pd-control-border); border-radius: 6px; color: var(--pd-text-muted); }.list-filter:focus-within { outline: 2px solid var(--pd-accent); outline-offset: 2px; }.list-filter input { width: 100%; border: 0; background: transparent; }.list-filter input:focus-visible { outline: none; }
  .tag-filter { display: flex; align-items: center; gap: 8px; padding: 0 12px 8px; }.tag-filter select { flex: 1; min-width: 0; }
  .list { flex: 1; min-height: 0; overflow: auto; padding: 4px 8px; }
  .session-row { display: flex; gap: 4px; align-items: center; border-radius: 8px; margin-bottom: 4px; padding-left: 4px; }.session-row.checked { outline: 1px solid var(--pd-control-border); background: var(--pd-surface); }.session-row > input { flex: none; }.session-row .sess { min-width: 0; }
  .sess { display: flex; flex-direction: column; justify-content: center; gap: 3px; width: 100%; min-height: var(--pd-recording-height); padding: 8px; text-align: left; border: 0; background: none; border-radius: 6px; }.sess:hover { background: var(--pd-hover); }.sess.active { background: var(--pd-selected); box-shadow: inset 3px 0 var(--pd-accent); }
  :global([data-density=compact]) .sess { padding-block: 6px; }
  .session-name { font-size: .875rem; font-weight: 600; overflow: hidden; max-width: 100%; text-overflow: ellipsis; white-space: nowrap; }.session-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; font-size: .75rem; color: var(--pd-text-muted); overflow-wrap: anywhere; min-width: 0; font-variant-numeric: tabular-nums; }.identity { width: 100%; justify-content: space-between; }.short-id { font-family: var(--pd-mono); }.container { font-family: var(--pd-mono); }.state { display: inline-flex; align-items: center; gap: 6px; }.status-dot { width: 6px; height: 6px; background: var(--pd-text-muted); border-radius: 50%; }.status-dot.live { background: var(--pd-accent); }
  .hintbox { padding: 24px 12px; color: var(--pd-text-muted); overflow-wrap: anywhere; }.list-foot, .enc-note { padding: 8px 12px; font-size: .75rem; color: var(--pd-text-muted); }.list-foot { border-top: 1px solid var(--pd-divider); }
  .selection-bar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; padding: var(--pd-toolbar-y) 16px; border-bottom: 1px solid var(--pd-divider); background: var(--pd-surface); font-size: .8125rem; }.selection-bar label, .list-select { display: flex; align-items: center; gap: 8px; font-size: .8125rem; }.list-select { padding: 4px 12px; min-height: 32px; }
  .resize-handle { background: var(--pd-sidebar); border-left: 1px solid var(--pd-divider); cursor: col-resize; touch-action: none; z-index: 1; }.resize-handle:hover, .resize-handle:focus-visible { background: var(--pd-control-border); }.resizing { user-select: none; cursor: col-resize; }
  .viewer { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: var(--pd-bg); }.viewer-head { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; padding: 16px 24px; border-bottom: 1px solid var(--pd-divider); }.heading { flex: 1; min-width: 140px; }h1 { font-size: 1.125rem; font-weight: 600; margin: 0 0 4px; overflow-wrap: anywhere; }.browse { display: flex; align-items: center; gap: 6px; }.header-actions { display: flex; align-items: center; gap: 4px; }
  .searchbar { display: flex; flex-wrap: wrap; gap: 8px; padding: var(--pd-toolbar-y) 24px; background: var(--pd-surface); }.scope-label { display: flex; align-items: center; }.scope-label select { max-width: 100%; }.searchwrap { display: flex; align-items: center; gap: 4px; flex: 1; min-width: min(280px, 100%); border: 1px solid var(--pd-control-border); border-radius: 6px; padding: 0 6px; color: var(--pd-text-muted); background: var(--pd-bg); }.searchwrap:focus-within { outline: 2px solid var(--pd-accent); outline-offset: 2px; }.searchwrap input { flex: 1; min-width: 24px; width: 0; border: 0; background: transparent; }.searchwrap input:focus-visible { outline: 0; }.counter { font-size: .75rem; white-space: nowrap; font-variant-numeric: tabular-nums; }.navbtn { background: transparent; border-color: transparent; min-width: 28px; padding: 4px; }.fonly { display: flex; gap: 8px; align-items: center; }.scope-note { padding: 0 24px 8px; background: var(--pd-surface); color: var(--pd-text-muted); font-size: .75rem; }
  .scroll { flex: 1; min-height: 0; overflow: auto; overflow-anchor: none; scrollbar-gutter: stable; }
  .execution { content-visibility: auto; contain-intrinsic-size: auto 200px; margin: 12px 16px; padding: 16px 20px 20px; border: 1px solid var(--pd-divider); border-radius: 8px; scroll-margin: 12px; background: var(--pd-terminal); }.execution.selected { border-color: var(--pd-accent); box-shadow: inset 3px 0 var(--pd-accent); }.deleted { text-decoration-color: var(--pd-text-muted); }.deleted .session-name, .execution.deleted .command { text-decoration: line-through; }
  .execution-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; min-width: 0; }.execution-meta { display: flex; flex-wrap: wrap; gap: 4px 12px; min-width: 0; flex: 1; color: var(--pd-text-muted); font: .75rem/1.5 var(--pd-mono); }.cwd { overflow-wrap: anywhere; min-width: 0; }.when { font-variant-numeric: tabular-nums; white-space: nowrap; }.entry-actions { display: flex; align-items: center; gap: 4px; flex: none; }.edited { font-size: .75rem; color: var(--pd-text-muted); }
  .command-line { display: grid; grid-template-columns: 16px minmax(0, 1fr) auto; align-items: start; gap: 8px; padding: 8px 12px; background: var(--pd-command); border: 1px solid var(--pd-divider); border-left: 2px solid var(--pd-accent); border-radius: 6px; }.prompt { color: var(--pd-accent); font: 600 .875rem/1.5 var(--pd-mono); padding-top: 6px; user-select: none; }.command { margin: 0; padding: 6px 0; min-width: 0; font: 600 .875rem/1.5 var(--pd-mono); white-space: pre-wrap; overflow-wrap: anywhere; color: var(--pd-text); }.noout { padding: 8px 24px; color: var(--pd-text-muted); font-size: .8125rem; }
  .viewer-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; padding: var(--pd-toolbar-y) 16px; border-top: 1px solid var(--pd-divider); background: var(--pd-surface); font-size: .75rem; color: var(--pd-text-muted); }.range { font-variant-numeric: tabular-nums; }.following { border-color: var(--pd-accent); }
  .details { padding: 16px 24px; border-bottom: 1px solid var(--pd-divider); color: var(--pd-text-muted); max-height: 40vh; overflow: auto; font-size: .8125rem; }dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; margin: 0; }dd { margin: 0; display: flex; align-items: center; gap: 8px; overflow-wrap: anywhere; min-width: 0; }.recording-actions { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; border-top: 1px solid var(--pd-divider); padding-top: 8px; }
  .error, .loading { padding: 8px 16px; background: var(--pd-surface); color: var(--pd-text-muted); }.error { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px; color: var(--pd-danger); }
  .hit { display: block; width: 100%; padding: 16px 24px; text-align: left; border: 0; border-bottom: 1px solid var(--pd-divider); border-radius: 0; background: var(--pd-terminal); content-visibility: auto; contain-intrinsic-size: auto 100px; }.current-hit { background: var(--pd-selected); box-shadow: inset 3px 0 var(--pd-accent); }.meta, .results-meta { font-size: .75rem; color: var(--pd-text-muted); }.results-meta { padding: 12px 24px; }.hitcmd, .hitsnip { display: block; font-family: var(--pd-mono); white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 8px; }.hitsnip { font-size: .8125rem; color: var(--pd-text-muted); }
  .welcome { min-height: 65%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; }.welcome strong { font-size: 1.125rem; }.rename-label { display: grid; gap: 8px; margin: 12px 0; }.edit-text { width: 100%; font: .8125rem/1.5 var(--pd-mono); resize: vertical; }.edit-error { color: var(--pd-danger); }.modal-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-top: 16px; }
  .source-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; padding: 16px; }.source-note { color: var(--pd-text-muted); padding: 0 16px; }.source-text { padding: 16px; margin: 0; background: var(--pd-terminal); font-size: .8125rem; white-space: pre-wrap; overflow-wrap: anywhere; }
  .narrow { grid-template-columns: minmax(0, 1fr); }.narrow .resize-handle { display: none; }.narrow .mobile-hidden { display: none; }
  @media (max-width: 1100px) { .viewer-head { padding: 12px 16px; }.searchbar { padding-inline: 16px; }.execution { margin: 10px 12px; padding: 12px 16px 16px; } }
  @media (max-width: 480px) { .viewer-head { padding: 8px; }.header-actions { margin-left: auto; }.searchbar, .selection-bar { padding-inline: 8px; }.execution { margin: 8px; padding: 12px 8px; }.command-line { padding: 6px 8px; }.entry-actions { gap: 0; }.edited, .follow-state { display: none; }.browse span { display: none; }.viewer-foot { padding-inline: 8px; } }
</style>
