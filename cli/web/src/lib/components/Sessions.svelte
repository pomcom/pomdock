<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { api } from '../api';
  import { app, setPref } from '../app.svelte';
  import { route, setParams } from '../router.svelte';
  import type { LogHit, Session } from '../types';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';

  let sessions: Session[] = $state([]);
  let current: Session | null = $state(null);
  let tagFilter = $state('');
  let query = $state('');
  let hits: LogHit[] | null = $state(null);
  let error = $state('');
  let loadingList = $state(false);
  let highlight = $state(''); // query to highlight inside the open session
  let expanded: Set<number> = $state(new Set());

  const collapsed = $derived(!!app.prefs.sessAsideCollapsed);
  const tags = $derived([...new Set(sessions.map((s) => s.tag).filter(Boolean))]);
  const listed = $derived(sessions.filter((s) => !tagFilter || s.tag === tagFilter));

  $effect(() => {
    const name = app.engagement;
    untrack(() => {
      current = null;
      tagFilter = '';
      query = '';
      hits = null;
      if (name) loadList(name);
    });
  });

  async function loadList(name: string) {
    loadingList = true;
    error = '';
    try {
      sessions = await api.sessions(name);
      const want = route.params.id;
      if (want && !current) await open(want);
    } catch (e) {
      error = (e as Error).message;
      sessions = [];
    } finally {
      loadingList = false;
    }
  }

  async function open(id: string, q = '', index = -1) {
    try {
      current = await api.session(app.engagement, id);
      error = '';
    } catch (e) {
      error = (e as Error).message;
      return;
    }
    expanded = new Set();
    highlight = q;
    setParams({ id });
    if (index >= 0) {
      expanded = new Set([index]);
      await tick();
      document.getElementById(`entry-${index}`)?.scrollIntoView({ block: 'center' });
    }
  }

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onsearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(async () => {
      const q = query.trim();
      if (!q) {
        hits = null;
        return;
      }
      try {
        hits = await api.logSearch(app.engagement, q);
      } catch (e) {
        error = (e as Error).message;
      }
    }, 180);
  }

  async function openHit(h: LogHit) {
    hits = null;
    await open(h.sessionId, query.trim(), h.index);
  }

  function toggle(i: number) {
    const next = new Set(expanded);
    if (next.has(i)) next.delete(i);
    else next.add(i);
    expanded = next;
  }

  const when = (e: { date: string; time: string }) => (e.time ? `${e.date} ${e.time}` : e.date ? `${e.date} (est.)` : 'time unknown');
</script>

<div class="sessions">
  <aside class:collapsed>
    <div class="head">
      <span>Sessions</span>
      <span class="grow"></span>
      <button class="sm ghost" title="re-read recordings" onclick={() => loadList(app.engagement)}>↻</button>
      <button class="sm ghost" title="hide list" onclick={() => setPref('sessAsideCollapsed', true)}>⇤</button>
    </div>
    {#if tags.length}
      <div class="tagbar">
        <button class="chip" class:on={tagFilter === ''} onclick={() => (tagFilter = '')}>all</button>
        {#each tags as t (t)}
          <button class="chip" class:on={tagFilter === t} onclick={() => (tagFilter = tagFilter === t ? '' : t)}>{t}</button>
        {/each}
      </div>
    {/if}
    <div class="list">
      {#each listed as s (s.id)}
        <button class="sess" class:active={current?.id === s.id} onclick={() => open(s.id)}>
          <span class="t">{s.title || s.id}</span>
          <span class="m">
            <span class="badge">{s.source === 'script' ? 'recorded' : 'imported'}</span>
            {#if s.tag}<span class="badge user">{s.tag}</span>{/if}
            <span>{s.date}</span>
          </span>
        </button>
      {:else}
        <div class="hintbox">
          {#if loadingList}loading…{:else}
            No sessions{tagFilter ? ` tagged “${tagFilter}”` : ''} yet.<br /><br />
            Every shell opened through pomdock is recorded automatically.<br />
            Label a section: <code>pdsession recon</code><br /><br />
            Import a paste:<br /><code>pomdock web import file.log -n {app.engagement}</code>
          {/if}
        </div>
      {/each}
    </div>
  </aside>

  <main>
    <div class="bar">
      {#if collapsed}
        <button class="sm ghost" title="show sessions list" onclick={() => setPref('sessAsideCollapsed', false)}>⇥</button>
      {/if}
      <input class="search" bind:value={query} oninput={onsearch} placeholder="Search all output & commands…" />
      {#if current}<span class="title">{current.title}</span>{/if}
    </div>
    <div class="scroll">
      {#if error}
        <div class="empty">error: {error}</div>
      {:else if hits}
        <div class="entry muted"><b class="count">{hits.length}</b> matches for “{query.trim()}”</div>
        {#each hits as h (`${h.sessionId}:${h.index}`)}
          <button class="entry hit" onclick={() => openHit(h)}>
            <span class="meta"><span class="when">{when(h)}</span><span class="badge">{h.inOutput ? 'output' : 'command'}</span><span>{h.title}</span></span>
            <span class="hitcmd">❯ <Highlight text={h.command} q={query} /></span>
            {#if h.snippet}<span class="hitsnip"><Highlight text={h.snippet} q={query} /></span>{/if}
          </button>
        {/each}
      {:else if current}
        {#each current.entries ?? [] as e, i}
          <div class="entry" id="entry-{i}">
            <div class="meta"><span class="when">{when(e)}</span>{#if e.cwd}<span class="cwd">{e.cwd}</span>{/if}</div>
            <div class="cmd"><span class="prompt">❯</span><span class="grow"><Highlight text={e.command} q={highlight} /></span><CopyButton text={e.command} title="copy command" /></div>
            {#if e.output.trim()}
              {@const long = e.output.split('\n').length > 4}
              <div class="outwrap">
                <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
                <pre class="log" class:collapsed={long && !expanded.has(i)} title={long ? 'click to expand' : ''}
                  onclick={() => long && toggle(i)}><Highlight text={e.output} q={highlight} /></pre>
                <CopyButton text={e.output} title="copy output" />
              </div>
            {:else}
              <div class="noout">(no output captured)</div>
            {/if}
          </div>
        {/each}
      {:else}
        <div class="empty">Pick a session, or search across all recorded output.</div>
      {/if}
    </div>
  </main>
</div>

<style>
  .sessions { display: grid; grid-template-columns: 300px 1fr; height: 100%; min-height: 0; }
  aside { border-right: 1px solid var(--surface1); background: var(--mantle); display: flex; flex-direction: column; min-height: 0; }
  aside.collapsed { display: none; }
  .sessions:has(aside.collapsed) { grid-template-columns: 1fr; }
  .head {
    display: flex; align-items: center; gap: 6px; padding: 9px 12px 6px; color: var(--overlay);
    font-size: 10px; text-transform: uppercase; letter-spacing: .6px; font-weight: 600;
  }
  .tagbar { display: flex; flex-wrap: wrap; gap: 6px; padding: 4px 12px 10px; }
  .chip { font-size: 11px; border-radius: 20px; padding: 2px 9px; background: var(--base); color: var(--subtext); }
  .chip.on { background: var(--accent); color: var(--base); border-color: var(--accent); font-weight: 600; }
  .list { overflow: auto; flex: 1; }
  .sess {
    display: block; width: 100%; text-align: left; border: none; border-radius: 0; background: none;
    border-bottom: 1px solid var(--surface0); padding: 10px 14px;
  }
  .sess:hover { background: var(--surface0); }
  .sess.active { background: color-mix(in srgb, var(--accent) 14%, transparent); box-shadow: inset 3px 0 0 var(--accent); }
  .t { display: block; font-weight: 600; font-size: 12.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .m { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; color: var(--overlay); font-size: 11px; margin-top: 3px; }
  .badge.user { background: color-mix(in srgb, var(--accent) 25%, transparent); color: var(--accent); }
  .hintbox { color: var(--overlay); padding: 20px 14px; font-size: 12px; }
  main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .search { width: 360px; max-width: 60vw; background: var(--base); }
  .title { color: var(--overlay); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .scroll { flex: 1; overflow: auto; padding: 4px 0 40px; }
  .entry { display: block; width: 100%; text-align: left; padding: 12px 22px; border: none; border-radius: 0; background: none; border-bottom: 1px solid var(--surface0); }
  .entry.hit:hover { background: var(--mantle); }
  .muted { color: var(--overlay); }
  .count { color: var(--accent); }
  .meta { display: flex; gap: 10px; align-items: center; color: var(--overlay); font-size: 11px; margin-bottom: 6px; }
  .when { color: var(--cyan); font-family: var(--mono); }
  .cwd { font-family: var(--mono); }
  .cmd { font-family: var(--mono); font-size: 13px; font-weight: 600; display: flex; gap: 8px; align-items: flex-start; }
  .prompt { color: var(--accent); }
  .outwrap { display: flex; gap: 8px; align-items: flex-start; margin-top: 8px; }
  .outwrap pre { flex: 1; }
  pre.collapsed { max-height: 6.5em; overflow: hidden; cursor: pointer; mask-image: linear-gradient(to bottom, #000 60%, transparent); }
  .noout { color: var(--faint); font-size: 11px; margin-top: 6px; font-style: italic; }
  .hitcmd { display: block; font-family: var(--mono); font-weight: 600; }
  .hitsnip { display: block; font-family: var(--mono); color: var(--subtext); font-size: 11.5px; margin-top: 3px; }
  @media (max-width: 760px) { .sessions { grid-template-columns: 1fr; } aside { max-height: 40vh; } }
</style>
