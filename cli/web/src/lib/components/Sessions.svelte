<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { api } from '../api';
  import { app, setPref, toast } from '../app.svelte';
  import { route, setParams } from '../router.svelte';
  import type { LogHit, Session, SessionEntry } from '../types';
  import { outputPreview, PAGE_SIZE, sessionKey, sessionName, sessionState, sessionWhen } from '../sessions';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';

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
  let searchVersion = 0;
  let openVersion = 0;
  let listVersion = 0;
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  const positions = new Map<string, { top: number; page: number; expanded: Set<number> }>();
  const names = $derived(app.prefs.sessionNames ?? {});
  const nameOf = (s: Session) => sessionName(s, names);
  const collapsed = $derived(app.prefs.sessAsideCollapsed ?? viewport < 1000);
  const asideWidth = $derived(Math.max(260, Math.min(300, app.prefs.sessAsideWidth ?? 280)));
  const tags = $derived([...new Set(sessions.map((s) => s.tag).filter(Boolean))]);
  const listed = $derived(sessions.filter((s) => (!tagFilter || s.tag === tagFilter) &&
    `${nameOf(s)} ${s.container ?? ''} ${s.date} ${sessionWhen(s)}`.toLowerCase().includes(listFilter.toLowerCase())));
  const entries: SessionEntry[] = $derived.by(() => current?.entries ?? []);
  const findTerm = $derived(findIn.trim().toLowerCase());
  type Occ = { e: number; field: 'cmd' | 'out'; pos: number };
  const occurrences = $derived.by<Occ[]>(() => {
    if (!findTerm) return [];
    const out: Occ[] = [];
    entries.forEach((en, e) => {
      for (const [field, text] of [['cmd', en.command], ['out', en.output ?? '']] as const) {
        const hay = text.toLowerCase();
        for (let pos = hay.indexOf(findTerm); pos >= 0; pos = hay.indexOf(findTerm, pos + findTerm.length)) out.push({ e, field, pos });
      }
    });
    return out;
  });
  const matchSet = $derived(new Set(occurrences.map((o) => o.e)));
  const shownEntries = $derived(onlyMatches && findTerm ? [...matchSet] : entries.map((_, i) => i));
  const visibleEntries = $derived(shownEntries.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE));
  const curOcc = $derived(occurrences[cur] ?? null);
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
    current = null;
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
      if (route.params.id && !current && result.some((s) => s.id === route.params.id)) await open(route.params.id);
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
      current = result;
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
      page = saved?.page ?? (follow ? Math.max(0, Math.ceil((result.entries?.length ?? 0) / PAGE_SIZE) - 1) : 0);
      if (index >= 0) { page = Math.floor(index / PAGE_SIZE); expanded = new Set([...expanded, index]); follow = false; }
      setParams({ id, archived: archived ? '1' : '' });
      await tick();
      if (scrollEl) scrollEl.scrollTop = follow ? scrollEl.scrollHeight : saved?.top ?? 0;
      if (index >= 0) document.getElementById(`entry-${index}`)?.scrollIntoView({ block: 'center' });
    } catch (e) { if (version === openVersion) error = (e as Error).message; }
    finally { if (version === openVersion) opening = false; }
  }

  async function refreshCurrent() {
    if (!current || opening) return;
    const id = current.id, engagement = app.engagement, arch = archived, version = openVersion;
    try {
      const result = await api.session(engagement, id, arch);
      if (version !== openVersion || current?.id !== id || engagement !== app.engagement || arch !== archived) return;
      current = result;
      sessions = sessions.map((s) => s.id === id ? { ...result, entries: undefined } : s);
      if (follow && !findTerm) {
        page = Math.max(0, Math.ceil((result.entries?.length ?? 0) / PAGE_SIZE) - 1);
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
      opening = loadingList = follow = false;
      page = 0;
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
    findIn = ''; onlyMatches = false; follow = true;
    page = Math.max(0, pageCount - 1);
    await tick();
    if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
  }

  function onscroll() {
    if (scrollEl && follow && scrollEl.scrollHeight - scrollEl.clientHeight - scrollEl.scrollTop > 48) follow = false;
  }

  function rename() {
    if (!current) return;
    const next = { ...names };
    const key = sessionKey(current);
    if (newName.trim()) next[key] = newName.trim(); else delete next[key];
    setPref('sessionNames', next);
    renaming = false;
  }

  function startResize(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    resizing = true;
  }
  function resize(e: PointerEvent) {
    if (!resizing) return;
    const rect = (e.currentTarget as HTMLElement).parentElement!.getBoundingClientRect();
    setPref('sessAsideWidth', Math.max(260, Math.min(300, e.clientX - rect.left)));
  }
  function resizeKey(e: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    setPref('sessAsideWidth', e.key === 'Home' ? 260 : e.key === 'End' ? 300 : Math.max(260, Math.min(300, asideWidth + (e.key === 'ArrowLeft' ? -10 : 10))));
  }
  const when = (e: { date: string; time: string }) => [e.date, e.time].filter(Boolean).join(' · ');
  function findKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); scope === 'session' ? goto(e.shiftKey ? -1 : 1) : gotoHit(e.shiftKey ? -1 : 1); }
    if (e.key === 'Escape') { stopSearch(); findIn = query = ''; hits = null; }
  }
  // --- encrypt (cold-store) ----------------------------------------------------
  let encryptedCount = $state(0);
  let encryptTarget: Session | null = $state(null); // session awaiting a passphrase
  let pass1 = $state('');
  let pass2 = $state('');
  let encrypting = $state(false);

  async function refreshEncryptedCount(name: string) {
    try {
      const result = await api.encryptedArchives(name);
      if (name === app.engagement) encryptedCount = result.length;
    } catch {
      if (name === app.engagement) encryptedCount = 0;
    }
  }

  function askEncrypt(s: Session) {
    encryptTarget = s;
    pass1 = '';
    pass2 = '';
  }

  async function doEncrypt() {
    if (!encryptTarget) return;
    if (!pass1 || pass1 !== pass2) {
      toast(pass1 ? 'passphrases do not match' : 'enter a passphrase');
      return;
    }
    const s = encryptTarget;
    encrypting = true;
    try {
      await api.encryptSession(app.engagement, s.id, pass1);
      current = null;
      openVersion++;
      setParams({ id: '' });
      encryptTarget = null;
      pass1 = pass2 = '';
      await loadList(app.engagement);
      await refreshEncryptedCount(app.engagement);
      toast('encrypted — restore with: pomdock web session-restore', undefined);
    } catch (e) {
      toast((e as Error).message);
    } finally {
      encrypting = false;
    }
  }


</script>

<svelte:window bind:innerWidth={viewport} />
<div class="sessions" class:list-hidden={collapsed} class:resizing style:--aside-width={`${asideWidth}px`}>
  {#if !collapsed}
    <aside aria-label="Session list">
      <div class="head"><strong>Recordings <span class="muted">{sessions.length}</span></strong><span class="grow"></span>
        <button class="sm ghost icon" title="Refresh recordings" aria-label="Refresh recordings" onclick={() => loadList(app.engagement)}><Icon name="refresh" size={14} /></button>
        <button class="sm ghost icon" title="Collapse session list" aria-label="Collapse session list" aria-expanded="true" onclick={() => setPref('sessAsideCollapsed', true)}><Icon name="panel" size={14} /></button>
      </div>
      <div class="scopes" aria-label="Recording collection"><button class:on={!archived} aria-pressed={!archived} onclick={() => setArchived(false)}>Recordings</button><button class:on={archived} aria-pressed={archived} onclick={() => setArchived(true)}>Archived</button></div>
      <div class="list-filter"><Icon name="search" size={14} /><input aria-label="Filter sessions by name, container or date" bind:value={listFilter} placeholder="Filter sessions…" /></div>
      {#if tags.length}<label class="tag-filter">Tag <select aria-label="Filter by tag" bind:value={tagFilter}><option value="">All tags</option>{#each tags as t}<option value={t}>{t}</option>{/each}</select></label>{/if}
      <div class="list" aria-busy={loadingList}>
        {#each listed as s (s.id)}
          <button class="sess" class:active={current?.id === s.id} aria-current={current?.id === s.id ? 'true' : undefined} onclick={() => open(s.id)}>
            <span class="session-name">{nameOf(s)}</span>
            <span class="session-meta">{#if s.container}<span class="container">{s.container}</span> · {/if}{sessionWhen(s)}</span>
            <span class="session-meta"><span class="status-dot" class:live={s.state === 'open'}></span>{sessionState(s)}</span>
          </button>
        {:else}<div class="hintbox">{#if loadingList}Loading recordings…{:else if listFilter || tagFilter}No sessions match these filters.{:else if archived}No archived recordings.{:else}No recordings yet. Open a terminal from <a href="#/containers">Containers</a> to begin.<br /><br />Label a section with <code>pdsession recon</code>, or import with <code>pomdock web import file.log -n {app.engagement}</code>.{/if}</div>{/each}
      </div>
      {#if encryptedCount}<div class="enc-note">{encryptedCount} encrypted. Restore via <code>pomdock web session-restore</code>.</div>{/if}
      <div class="list-foot">{archived ? 'Archived captures' : 'Local recordings & imported transcripts'}</div>
    </aside>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
    <div class="resize-handle" role="separator" aria-label="Resize session list" aria-orientation="vertical" aria-valuemin={260} aria-valuemax={300} aria-valuenow={asideWidth} tabindex="0" onpointerdown={startResize} onpointermove={resize} onpointerup={() => resizing = false} onlostpointercapture={() => resizing = false} onkeydown={resizeKey}></div>
  {/if}
  <section class="viewer" aria-label="Session viewer" aria-busy={opening}>
    <div class="viewer-head">
      {#if collapsed}<button class="sm ghost icon" title="Expand session list" aria-label="Expand session list" aria-expanded="false" onclick={() => setPref('sessAsideCollapsed', false)}><Icon name="panel" /></button>{/if}
      <div class="heading"><h1>{current ? nameOf(current) : 'Session history'}</h1><div class="session-meta">
        {#if current}<span class="state"><span class="status-dot" class:live={current.state === 'open'}></span>{sessionState(current)}</span>
          {#if current.container}<span class="container">{current.container}</span>{/if}
          {#if sessionWhen(current)}<span>Recorded {sessionWhen(current)}</span>{/if}
          {#if current.ended}<span>Ended {new Date(current.ended * 1000).toLocaleTimeString()}</span>{/if}<span>{entries.length} executions</span>
        {:else}Commands and output from your local terminal recordings{/if}
      </div></div>
      {#if current}<div class="header-actions"><button class="sm ghost icon" title="Refresh session" aria-label="Refresh session" onclick={refreshCurrent}><Icon name="refresh" size={14} /></button><button class="sm ghost" aria-expanded={details} onclick={() => details = !details}>Details</button><button class="sm ghost" onclick={() => { newName = names[sessionKey(current!)] ?? ''; renaming = true; }}>Rename</button></div>{/if}
    </div>
    {#if current && details}<div class="details">
      <dl><dt>Recording ID</dt><dd><code>{current.id}</code><CopyButton text={current.id} title="Copy recording ID" /></dd><dt>Source</dt><dd>{current.source === 'script' ? 'Terminal capture' : 'Imported transcript'}</dd><dt>Container</dt><dd>{current.container || 'Not recorded in this capture'}</dd><dt>Engagement</dt><dd>{current.engagement}</dd></dl>
      {#if current.state === 'open'}<p>An open capture has no end marker. Its terminal may still be running; interrupted captures can also remain open.</p>{/if}
      <p>Command timing is shown when recorded. Exit status and duration are unavailable in this capture format.</p>
      <div class="recording-actions"><span>Entire capture, including tagged sections</span><button class="sm" onclick={() => current && toggleArchive(current)}>{archived ? 'Restore recording' : 'Archive recording'}</button>{#if archived}<button class="sm danger" onclick={() => current && askEncrypt(current)}>Encrypt &amp; remove plaintext…</button>{/if}</div>
    </div>{/if}
    <div class="searchbar">
      <label class="scope-label">Search <select aria-label="Search scope" value={scope} onchange={(e) => setScope(e.currentTarget.value as 'session' | 'all')}><option value="session" disabled={!current}>Current session</option><option value="all">All recordings</option></select></label>
      <div class="searchwrap"><Icon name="search" size={14} />
        {#if scope === 'all'}<input bind:this={findEl} aria-label="Search all non-archived recordings in this engagement" bind:value={query} oninput={onsearch} onkeydown={findKeydown} placeholder="Commands & output…" />{:else}<input bind:this={findEl} aria-label="Search commands and output in current session" bind:value={findIn} onkeydown={findKeydown} placeholder="Commands & output…" />{/if}
        {#if (scope === 'session' && findTerm) || (scope === 'all' && query.trim())}
          <span class="counter" role="status">{scope === 'session' ? (occurrences.length ? `${cur + 1} / ${occurrences.length}` : '0 matches') : searching ? 'Searching…' : hits?.length ? `${hitIndex + 1} / ${hits.length}` : '0 matches'}</span>
          <button class="navbtn" title="Previous match (Shift+Enter)" aria-label="Previous match" disabled={scope === 'session' ? !occurrences.length : !hits?.length} onclick={() => scope === 'session' ? goto(-1) : gotoHit(-1)}>↑</button><button class="navbtn" title="Next match (Enter)" aria-label="Next match" disabled={scope === 'session' ? !occurrences.length : !hits?.length} onclick={() => scope === 'session' ? goto(1) : gotoHit(1)}>↓</button><button class="navbtn" title="Clear search" aria-label="Clear search" onclick={() => { stopSearch(); findIn = query = ''; hits = null; }}>×</button>
        {/if}
      </div>
      {#if scope === 'session' && findTerm}<label class="fonly"><input type="checkbox" bind:checked={onlyMatches} onchange={() => { page = 0; tick().then(() => goto(0)); }} /> Matches only</label>{/if}
    </div>
    {#if scope === 'all'}<div class="scope-note">Searches this engagement’s non-archived recordings.</div>{/if}
    {#if error}<div class="error" role="alert">{error}<button class="sm ghost" onclick={() => { error = ''; current ? refreshCurrent() : loadList(app.engagement); }}>Retry</button></div>{/if}
    {#if opening}<div class="loading" role="status">Opening recording…</div>{/if}
    <div class="scroll" bind:this={scrollEl} {onscroll}>
      {#if searching}<div class="empty">Searching recordings…</div>
      {:else if hits !== null}
        <div class="results-meta">{hits.length} matching executions for “{query.trim()}”</div>
        {#each hits as h, hi (`${h.sessionId}:${h.index}`)}<button class="entry hit" class:current-hit={hitIndex === hi} id="hit-{hi}" onclick={() => openHit(h)} onfocus={() => hitIndex = hi}>
          <span class="meta">{nameOf(sessions.find((s) => s.id === h.sessionId) ?? { id: h.sessionId, title: h.title, source: 'script', engagement: app.engagement, tag: '', date: h.date, created: 0 })}{#if when(h)} · {when(h)}{/if} · {h.inOutput ? 'Output match' : 'Command match'}</span><span class="hitcmd"><Highlight text={h.command} q={query} /></span>{#if h.snippet}<span class="hitsnip"><Highlight text={h.snippet} q={query} /></span>{/if}
        </button>{:else}<div class="empty">No matches for “{query.trim()}”. Try a shorter command, path, or output fragment.</div>{/each}
      {:else if current}
        {#if findTerm && !occurrences.length}<div class="empty" role="status">No commands or output match “{findIn.trim()}” in this session.</div>{/if}
        {#if !entries.length}<div class="empty">This session has no recorded executions yet.</div>{/if}
        {#each visibleEntries as i (i)}
          {@const e = entries[i]}{@const output = e.output ?? ''}{@const preview = outputPreview(output)}{@const long = preview.length < output.length}{@const forcedOpen = curOcc?.e === i && curOcc?.field === 'out'}{@const show = expanded.has(i) || forcedOpen}
          <article class="entry execution" id="entry-{i}" aria-label={`Execution ${i + 1}`}>
            <div class="execution-head">
              <span class="prompt" aria-hidden="true">❯</span>
              <div class="command-line">
                <pre class="command"><Highlight text={e.command} q={findIn} curStart={curOcc?.e === i && curOcc.field === 'cmd' ? curOcc.pos : -1} curId="find-cur" /></pre>
                {#if when(e)}<time class="when">{when(e)}</time>{/if}
              </div>
              <div class="exec-actions">
                <CopyButton text={e.command} title="Copy command" />
                {#if output}<CopyButton text={output} title="Copy output" />{/if}
              </div>
            </div>
            {#if e.cwd}<div class="cwd">{e.cwd}</div>{/if}
            {#if output.length}<pre class="output"><Highlight text={show ? output : preview} q={findIn} curStart={curOcc?.e === i && curOcc.field === 'out' ? curOcc.pos : -1} curId="find-cur" /></pre>{#if long}<button class="output-toggle" aria-expanded={!!show} disabled={forcedOpen} onclick={() => toggle(i)}>{forcedOpen ? 'Full output shown for search match' : show ? 'Collapse output' : `Show full output · ${(output.length - preview.length).toLocaleString()} characters hidden`}</button>{/if}
            {:else}<div class="noout">{e.output == null ? 'Output unavailable in this recording' : 'No output captured'}</div>{/if}
          </article>
        {/each}
      {:else}<div class="empty welcome"><Icon name="sessions" size={28} /><strong>Choose a recording</strong><span>Review commands and output, or search this engagement’s recordings.</span><a href="#/shells">View open terminals →</a></div>{/if}
    </div>
    {#if current && hits === null && !searching}<div class="viewer-foot"><span class="range">{shownEntries.length ? `${page * PAGE_SIZE + 1}–${Math.min((page + 1) * PAGE_SIZE, shownEntries.length)} of ${shownEntries.length}` : '0'} executions</span>{#if pageCount > 1}<button class="sm ghost" disabled={page === 0} onclick={() => changePage(page - 1)}>Previous</button><button class="sm ghost" disabled={page >= pageCount - 1} onclick={() => changePage(page + 1)}>Next</button>{/if}<span class="grow"></span>{#if current.state === 'open' && !archived}<span class="follow-state">{follow ? 'Following' : 'Follow paused'}</span><button class="sm" class:following={follow} aria-pressed={follow} onclick={() => follow ? follow = false : resumeFollow()}>{follow ? 'Pause following' : 'Follow output ↓'}</button>{/if}</div>{/if}
  </section>
</div>
{#if renaming && current}<Modal title="Rename session" onclose={() => renaming = false}><form onsubmit={(e) => { e.preventDefault(); rename(); }}><p class="muted">Display name saved locally in pom dock preferences. Leave blank to use the recorded name.</p><label class="rename-label">Session name <input bind:value={newName} placeholder={sessionName(current)} maxlength="160" /></label><div class="modal-actions"><button type="submit" class="accent">Save name</button></div></form></Modal>{/if}
{#if encryptTarget}<Modal title="Encrypt & cold-store" busy={encrypting} onclose={() => { if (!encrypting) { encryptTarget = null; pass1 = pass2 = ''; } }}><p class="muted">Encrypts the capture containing <b>{nameOf(encryptTarget)}</b> and removes the plaintext. Restore only with this passphrase via <code>pomdock web session-restore</code>. There is no recovery if you lose it.</p><form onsubmit={(e) => { e.preventDefault(); doEncrypt(); }}><label class="rename-label">Passphrase <input type="password" autocomplete="new-password" bind:value={pass1} disabled={encrypting} /></label><label class="rename-label">Confirm passphrase <input type="password" autocomplete="new-password" bind:value={pass2} disabled={encrypting} /></label><div class="modal-actions"><button type="submit" class="danger" disabled={encrypting || !pass1 || pass1 !== pass2}>{encrypting ? 'Encrypting…' : 'Encrypt & remove plaintext'}</button></div></form></Modal>{/if}

<style>
  .sessions { position: relative; display: grid; grid-template-columns: var(--aside-width) 5px minmax(0, 1fr); height: 100%; min-height: 0; }
  .sessions.list-hidden { grid-template-columns: minmax(0, 1fr); }
  aside { background: var(--mantle); display: flex; flex-direction: column; min-height: 0; min-width: 0; }
  .head { display: flex; align-items: center; gap: 3px; padding: 12px 12px 8px; font-size: 12px; }
  .head strong { font-weight: 600; }.head .muted { margin-left: 6px; font-weight: 400; }
  .icon { display: inline-flex; align-items: center; justify-content: center; padding: 4px; min-width: 28px; }
  .muted { color: var(--overlay); }
  .scopes { display: flex; gap: 4px; margin: 0 12px 10px; border-bottom: 1px solid var(--surface1); }
  .scopes button { flex: 1; border: 0; background: none; border-radius: 0; padding: 6px; color: var(--subtext); }
  .scopes button.on { color: var(--accent); box-shadow: inset 0 -2px var(--accent); }
  .list-filter { display: flex; align-items: center; gap: 6px; margin: 0 12px 10px; border: 1px solid var(--surface1); border-radius: 5px; padding-left: 8px; color: var(--overlay); }
  .list-filter input { width: 100%; min-width: 0; border: none; background: transparent; padding: 6px; }
  .tag-filter { display: flex; align-items: center; gap: 8px; padding: 0 12px 10px; }.tag-filter select { flex: 1; min-width: 0; }
  .list { flex: 1; min-height: 0; overflow: auto; padding: 0 6px; }
  .sess { width: 100%; display: flex; flex-direction: column; gap: 3px; text-align: left; padding: 10px 12px; border: 0; background: none; border-radius: 4px; margin-bottom: 2px; }
  .sess:hover { background: var(--surface0); }.sess.active { background: var(--surface0); box-shadow: inset 2px 0 var(--accent); }
  .session-name { font-size: 12px; font-weight: 550; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .session-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; color: var(--overlay); font-size: 11px; line-height: 1.6; min-width: 0; overflow-wrap: anywhere; }
  .state { display: inline-flex; align-items: center; gap: 5px; }.container { font-family: var(--mono); color: var(--subtext); }
  .status-dot { width: 5px; height: 5px; background: var(--overlay); border-radius: 50%; flex: none; }.status-dot.live { background: var(--accent); }
  .hintbox { padding: 20px 12px; color: var(--subtext); font-size: 12px; overflow-wrap: anywhere; }
  .list-foot, .enc-note { padding: 10px 12px; color: var(--overlay); font-size: 10px; }.list-foot { border-top: 1px solid var(--surface1); }
  .resize-handle { background: var(--mantle); border-left: 1px solid var(--surface1); cursor: col-resize; touch-action: none; z-index: 1; }.resize-handle:hover, .resize-handle:focus-visible { background: var(--accent); }.resizing { user-select: none; cursor: col-resize; }
  .viewer { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .viewer-head { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; min-height: 76px; padding: 14px 20px; border-bottom: 1px solid var(--surface1); }
  .heading { flex: 1; min-width: 120px; }h1 { font-size: 15px; font-weight: 600; margin: 0 0 4px; overflow-wrap: anywhere; }.header-actions { display: flex; align-items: center; gap: 3px; }
  .searchbar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; padding: 10px 20px; background: var(--mantle); }.scope-label { display: flex; align-items: center; gap: 6px; font-size: 11px; }.scope-label select { font-size: 11px; background: var(--base); padding: 5px 6px; }
  .searchwrap { display: flex; align-items: center; gap: 4px; flex: 1; min-width: min(260px, 100%); background: var(--base); border: 1px solid var(--surface1); border-radius: 5px; padding: 0 6px; color: var(--overlay); }.searchwrap:focus-within { border-color: var(--accent); }.searchwrap input { flex: 1; width: 0; min-width: 40px; border: none; background: none; padding: 6px; }.searchwrap input:focus { box-shadow: none; }
  .counter { font-size: 10px; font-variant-numeric: tabular-nums; white-space: nowrap; }.navbtn { border: 0; background: none; color: var(--subtext); padding: 3px; min-width: 24px; min-height: 26px; }.fonly { display: flex; align-items: center; gap: 5px; white-space: nowrap; font-size: 11px; }.scope-note { background: var(--mantle); color: var(--overlay); font-size: 10px; padding: 0 20px 8px; }
  .scroll { flex: 1; min-height: 0; overflow: auto; overflow-anchor: auto; scrollbar-gutter: stable; }.entry { border-bottom: 1px solid var(--surface1); }.execution { content-visibility: auto; contain-intrinsic-size: auto 180px; padding: 16px 20px; scroll-margin: 12px; }.execution-head { display: grid; grid-template-columns: 12px minmax(0, 1fr) auto; align-items: start; gap: 9px; min-width: 0; }
  .prompt { color: var(--accent); font-family: var(--mono); font-size: 14px; font-weight: 600; line-height: 1.65; user-select: none; }
  .command-line { display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px 12px; min-width: 0; }
  .command { flex: 0 1 auto; min-width: 0; max-width: 100%; margin: 0; font-family: var(--mono); font-size: 13px; font-weight: 600; line-height: 1.65; color: var(--text); white-space: pre-wrap; overflow-wrap: anywhere; }
  .when { flex: none; font-family: var(--mono); font-size: 11px; font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1.65; color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); border-radius: 4px; padding: 2px 7px; white-space: nowrap; }
  .exec-actions { display: flex; flex: none; gap: 2px; opacity: 0; transition: opacity .1s; }.execution:hover .exec-actions, .execution:focus-within .exec-actions { opacity: 1; }
  .output { font-family: var(--mono); font-size: 12px; line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere; color: var(--subtext); background: var(--crust); padding: 10px 12px; margin: 9px 0 0 17px; border-radius: 4px; tab-size: 8; }
  .cwd { font-family: var(--mono); font-size: 10px; color: var(--overlay); margin: 2px 0 0 17px; overflow-wrap: anywhere; }.output-toggle { margin: 4px 0 0 17px; padding: 4px 0; min-height: 24px; background: none; border: none; font-size: 11px; color: var(--accent); }.output-toggle:disabled { opacity: 1; color: var(--overlay); }.noout { color: var(--overlay); margin: 5px 0 0 17px; font-size: 11px; }
  .viewer-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; padding: 7px 16px; min-height: 44px; border-top: 1px solid var(--surface1); background: var(--mantle); font-size: 10px; color: var(--overlay); }.range { font-variant-numeric: tabular-nums; }.following { color: var(--accent); border-color: var(--accent); }
  .details { padding: 10px 20px; border-bottom: 1px solid var(--surface1); font-size: 11px; color: var(--subtext); max-height: 35vh; overflow: auto; }dl { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 6px 14px; margin: 0; }dt { color: var(--overlay); }dd { margin: 0; display: flex; align-items: center; gap: 5px; min-width: 0; overflow-wrap: anywhere; }dd code { overflow-wrap: anywhere; }.recording-actions { border-top: 1px solid var(--surface1); padding-top: 8px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }.recording-actions span { flex: 1; }
  .loading, .error { padding: 8px 20px; color: var(--subtext); background: var(--mantle); }.error { color: var(--red); display: flex; gap: 8px; justify-content: space-between; align-items: center; }
  .hit { display: block; width: 100%; text-align: left; padding: 14px 20px; border: none; border-radius: 0; background: none; content-visibility: auto; contain-intrinsic-size: auto 100px; }.current-hit { box-shadow: inset 2px 0 var(--accent); background: var(--mantle); }.meta, .results-meta { color: var(--overlay); font-size: 11px; }.results-meta { padding: 12px 20px; }.hitcmd, .hitsnip { display: block; font-family: var(--mono); white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 4px; }.hitsnip { font-size: 11px; color: var(--subtext); }
  .welcome { min-height: 60%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; }.welcome strong { font-size: 14px; color: var(--text); font-weight: 500; }.rename-label { display: grid; gap: 6px; margin: 12px 0; }.modal-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
  @media (hover: none) { .exec-actions { opacity: 1; } }
  @media (max-width: 1100px) { .viewer-head, .searchbar { padding-left: 14px; padding-right: 14px; }.execution { padding: 14px; } }
  @media (max-width: 700px) { .sessions:not(.list-hidden) { grid-template-columns: 260px 5px minmax(0, 1fr); }.sessions:not(.list-hidden) .viewer { visibility: hidden; }.sessions:not(.list-hidden) aside { position: absolute; inset: 0 auto 0 0; width: min(300px, 100%); z-index: 2; border-right: 1px solid var(--surface1); }.exec-actions { opacity: 1; }.follow-state { display: none; } }
</style>
