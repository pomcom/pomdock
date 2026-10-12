<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api';
  import { app, copyText, setPref, toast } from '../app.svelte';
  import {
    activeColumns, buildItems, mergeItems, noiseMatcher, rowItem, ruleFor, serialize, toTSV,
    type Defaults, type Fields, type Item, type SavedState,
  } from '../tracker';
  import type { ReportRow } from '../types';
  import ColumnsModal from './ColumnsModal.svelte';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import NoiseRulesModal from './NoiseRulesModal.svelte';

  // --- data + persistence ------------------------------------------------------

  let rowsById = new Map<string, ReportRow>();
  let items: Item[] = $state([]);
  let loading = $state(false);
  let error = $state('');
  let saveStatus: '' | 'saved' | 'saving' | 'error' = $state('');
  let loadedFor = '';
  let lastSaved = '';
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  let pendingSave: (() => Promise<void>) | null = null;

  const defaults: Defaults = $derived({
    tester: app.prefs.tester || app.meta?.tester || 'LS',
    source: app.prefs.source ?? '',
  });

  $effect(() => {
    const name = app.engagement;
    if (name) untrack(() => load(name));
  });

  async function load(name: string) {
    await flushSave();
    loading = true;
    error = '';
    try {
      const [rows, saved] = await Promise.all([api.rows(name), api.trackerState<SavedState>(name)]);
      if (name !== app.engagement) return; // switched away meanwhile
      rowsById = new Map(rows.map((r) => [r.id, r]));
      items = buildItems(rows, saved?.version === 1 ? saved : null, defaults);
      lastSaved = JSON.stringify(serialize(items, rowsById, defaults));
      loadedFor = name;
      saveStatus = 'saved';
      lastClicked = -1;
    } catch (e) {
      error = (e as Error).message;
      items = [];
      loadedFor = '';
    } finally {
      loading = false;
    }
  }

  // Every edit is saved (debounced) to <loot>/.pomdock/tracker.json.
  $effect(() => {
    const json = JSON.stringify(serialize(items, rowsById, defaults));
    if (!loadedFor || json === lastSaved) return;
    const name = loadedFor;
    saveStatus = 'saving';
    clearTimeout(saveTimer);
    pendingSave = async () => {
      pendingSave = null;
      try {
        await api.saveTrackerState(name, JSON.parse(json));
        lastSaved = json;
        saveStatus = 'saved';
      } catch (e) {
        saveStatus = 'error';
        toast(`saving the tracker failed: ${(e as Error).message}`);
      }
    };
    saveTimer = setTimeout(() => pendingSave?.(), 600);
  });

  async function flushSave() {
    clearTimeout(saveTimer);
    await pendingSave?.();
  }

  function onbeforeunload(e: BeforeUnloadEvent) {
    if (saveStatus === 'saving') {
      void flushSave();
      e.preventDefault();
    }
  }

  // --- view state --------------------------------------------------------------

  let query = $state('');
  let showNoise = $state(false);
  let lastClicked = -1;
  let noiseModal: string | null = $state(null); // prefill text while open
  let colsModal = $state(false);
  let searchEl: HTMLInputElement | undefined = $state();

  const hiddenCols = $derived(app.prefs.hiddenCols ?? ['mitre']);
  const cols = $derived(activeColumns(hiddenCols));
  const rules = $derived(app.prefs.noiseRules ?? []);
  const isRuleNoise = $derived(noiseMatcher(rules));
  const isNoise = (it: Item) => it.noise || isRuleNoise(it.command);

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return items
      .filter((it) => !it.deleted)
      .filter((it) => showNoise || it.incl || !isNoise(it))
      .filter((it) => !q || `${it.command} ${it.measure} ${it.targets}`.toLowerCase().includes(q))
      .sort((a, b) => a.ts - b.ts);
  });
  const checked = $derived(items.filter((it) => it.incl && !it.deleted));
  const numbers = $derived(new Map([...checked].sort((a, b) => a.ts - b.ts).map((it, i) => [it.key, i + 1])));
  const noiseCount = $derived(items.filter((it) => !it.deleted && isNoise(it)).length);
  const deletedCount = $derived(items.filter((it) => it.deleted).length);
  const allVisibleChecked = $derived(visible.length > 0 && visible.every((it) => it.incl));

  // --- actions -----------------------------------------------------------------

  function toggleRow(idx: number, shift: boolean) {
    const it = visible[idx];
    if (!it) return;
    const target = !it.incl;
    if (shift && lastClicked >= 0) {
      const [a, b] = [Math.min(lastClicked, idx), Math.max(lastClicked, idx)];
      for (let i = a; i <= b; i++) visible[i].incl = target;
    } else {
      it.incl = target;
    }
    lastClicked = idx;
  }

  function deleteItems(list: Item[]) {
    if (!list.length) return toast('nothing checked');
    for (const it of list) it.deleted = true;
    toast(`removed ${list.length} row${list.length > 1 ? 's' : ''} (atuin untouched)`, () => {
      for (const it of list) it.deleted = false;
    });
  }

  function restoreAll() {
    for (const it of items) it.deleted = false;
    toast('restored');
  }

  function bulkSet(field: keyof Fields, value: string) {
    if (!checked.length) return toast('check some rows first');
    for (const it of checked) (it as Record<string, unknown>)[field] = value;
    toast(`${checked.length} rows → ${field}${value ? `: ${value}` : ' cleared'}`);
  }

  function merge() {
    if (checked.length < 2) return toast('check 2+ rows to merge');
    const merged = mergeItems(checked, defaults);
    const gone = new Set(checked.map((it) => it.key));
    items = [...items.filter((it) => !gone.has(it.key)), merged];
    lastClicked = -1;
    toast(`merged ${gone.size} rows`);
  }

  function unmerge(m: Item) {
    const restored = m.ids.map((id) => rowsById.get(id)).filter((r) => r !== undefined)
      .map((r) => rowItem(r, defaults, { incl: true }));
    items = [...items.filter((it) => it.key !== m.key), ...restored];
    toast(`split back into ${restored.length} rows`);
  }

  function setRules(next: string[]) {
    setPref('noiseRules', next);
  }

  function markCheckedNoise() {
    if (!checked.length) return toast('check some rows first');
    const next = [...rules];
    for (const it of checked) {
      const r = ruleFor(it.command);
      if (r && !next.includes(r)) next.push(r);
    }
    setRules(next);
    toast(`added ${next.length - rules.length} noise rules`);
  }

  function tsv() {
    return toTSV(items, cols, !!app.prefs.withHeader);
  }

  async function copyTSV() {
    const text = tsv();
    if (!text) return toast('nothing checked');
    await copyText(text);
    toast(`copied ${checked.length} rows — paste into Excel`);
  }

  function download() {
    const text = tsv();
    if (!text) return toast('nothing checked');
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/tab-separated-values' }));
    a.download = `${app.engagement || 'report'}-report.tsv`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  // Bulk-bar inputs; source/tester also become the defaults for new rows.
  let bulkSource = $state(untrack(() => app.prefs.source ?? ''));
  let bulkTester = $state('');
  let bulkMitre = $state('');

  function onkeydown(e: KeyboardEvent) {
    const el = document.activeElement;
    if (e.key === '/' && !(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement)) {
      e.preventDefault();
      searchEl?.focus();
    }
    if (e.key === 'Escape' && el === searchEl) {
      query = '';
      searchEl?.blur();
    }
  }

  function grow(node: HTMLTextAreaElement) {
    const fit = () => {
      node.style.height = 'auto';
      node.style.height = `${Math.max(56, node.scrollHeight)}px`;
    };
    fit();
    node.addEventListener('input', fit);
    return { destroy: () => node.removeEventListener('input', fit) };
  }
</script>

<svelte:window {onkeydown} {onbeforeunload} />

<div class="tracker">
  <div class="bar">
    <div class="search">
      <input bind:this={searchEl} bind:value={query} placeholder="Search command, measure, target…" />
      {#if query}<span class="hint">{visible.length} match</span>{:else}<span class="hint kbd">/</span>{/if}
    </div>
    <label><input type="checkbox" bind:checked={showNoise} /> show noise ({noiseCount})</label>
    <button class="sm ghost" onclick={() => (colsModal = true)}>Columns</button>
    <button class="sm ghost" onclick={() => (noiseModal = '')}>Noise rules</button>
    <button class="sm ghost" onclick={() => setPref('bulkHidden', !app.prefs.bulkHidden)}>
      {app.prefs.bulkHidden ? 'Show' : 'Hide'} edit bar
    </button>
    <button class="sm ghost" title="re-read the atuin history (keeps your edits)" onclick={() => load(app.engagement)}>↻ refresh</button>
    {#if deletedCount}
      <span class="muted">{deletedCount} removed · <button class="link" onclick={restoreAll}>restore all</button></span>
    {/if}
    <span class="grow"></span>
    <span class="save save-{saveStatus}" title="curation is saved to <loot>/.pomdock/tracker.json">
      {saveStatus === 'saving' ? 'saving…' : saveStatus === 'error' ? 'save failed' : saveStatus === 'saved' ? 'saved' : ''}
    </span>
    <span class="pill">included <b>{checked.length}</b></span>
    <label title="prepend the team column headers">
      <input type="checkbox" checked={!!app.prefs.withHeader} onchange={(e) => setPref('withHeader', e.currentTarget.checked)} /> header row
    </label>
    <button class="accent" onclick={copyTSV}>Copy TSV</button>
    <button class="ghost" onclick={download}>Download .tsv</button>
  </div>

  {#if !app.prefs.bulkHidden}
    <div class="bar bulk">
      <span>Bulk edit <b class="count">{checked.length}</b> checked</span>
      <div class="group">
        <span class="glabel">Ext/Int</span>
        <button class="sm int" onclick={() => bulkSet('ie', 'Internal')}>Internal</button>
        <button class="sm ext" onclick={() => bulkSet('ie', 'External')}>External</button>
        <button class="sm ghost" onclick={() => bulkSet('ie', '')}>clear</button>
      </div>
      <div class="group">
        <span class="glabel">Source</span>
        <input bind:value={bulkSource} placeholder="192.0.2.5" size="15"
          onkeydown={(e) => e.key === 'Enter' && (bulkSet('source', bulkSource), setPref('source', bulkSource))} />
        <button class="sm" onclick={() => { bulkSet('source', bulkSource); setPref('source', bulkSource); }}>set</button>
      </div>
      {#if !hiddenCols.includes('tester')}
        <div class="group">
          <span class="glabel">Tester</span>
          <input bind:value={bulkTester} placeholder={defaults.tester} size="5" />
          <button class="sm" onclick={() => { const v = bulkTester || defaults.tester; bulkSet('tester', v); setPref('tester', v); }}>set</button>
        </div>
      {/if}
      {#if !hiddenCols.includes('mitre')}
        <div class="group">
          <span class="glabel">MITRE</span>
          <input bind:value={bulkMitre} placeholder="T1552.001" size="10" />
          <button class="sm" onclick={() => bulkSet('mitre', bulkMitre)}>set</button>
        </div>
      {/if}
      <div class="group">
        <span class="glabel">Outcome</span>
        <button class="sm" onclick={() => bulkSet('outcome', 'Successful')}>Successful</button>
        <button class="sm" onclick={() => bulkSet('outcome', 'Failed')}>Failed</button>
      </div>
      <span class="grow"></span>
      <button class="sm ghost" title="add the checked commands as noise rules" onclick={markCheckedNoise}>Mark noise</button>
      <button class="sm danger" onclick={() => deleteItems(checked)}>Delete checked</button>
      <button class="sm" onclick={merge}>Merge checked</button>
    </div>
  {/if}

  <div class="scroll">
    {#if error}
      <div class="empty">error: {error}</div>
    {:else if loading && !items.length}
      <div class="empty">loading…</div>
    {:else if !app.engagement}
      <div class="empty">No engagement with an atuin history found yet. Open a shell with <code>pomdock docker exec --name NAME</code>.</div>
    {:else if !visible.length}
      <div class="empty">No rows match. Clear the search or turn on “show noise”.</div>
    {:else}
      <table>
        <thead>
          <tr>
            <th class="chk">
              <input type="checkbox" title="select all visible" checked={allVisibleChecked}
                onchange={(e) => { for (const it of visible) it.incl = e.currentTarget.checked; }} />
            </th>
            {#each cols as c (c.key)}<th>{c.short}</th>{/each}
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each visible as it, idx (it.key)}
            <tr class="row" class:incl={it.incl} class:noise={isNoise(it)}>
              <td class="chk"><input type="checkbox" checked={it.incl} onclick={(e) => toggleRow(idx, e.shiftKey)} /></td>
              {#each cols as c (c.key)}
                {#if c.key === 'num'}
                  <td class="num" onclick={() => toggleRow(idx, false)}>{it.incl ? numbers.get(it.key) : ''}</td>
                {:else if c.key === 'started' || c.key === 'ended'}
                  <td class="ro" onclick={(e) => toggleRow(idx, e.shiftKey)}>{it[c.key]}</td>
                {:else if c.key === 'measure'}
                  <td class="measure">
                    <div class="mcell">
                      <button class="chev" class:open={it.expanded} title="show full command" onclick={() => (it.expanded = !it.expanded)}>▶</button>
                      <button class="mtext" class:edited={it.measure !== it.origMeasure} title="click to expand / edit" onclick={() => (it.expanded = true)}>
                        {#if it.kind === 'merged'}<span class="badge">merged {it.ids.length}</span>{/if}
                        <Highlight text={it.measure} q={query} />
                      </button>
                    </div>
                  </td>
                {:else if c.key === 'ie'}
                  <td class="eint">
                    <select class="cell" data-v={it.ie} bind:value={it.ie}>
                      <option value="">—</option><option>Internal</option><option>External</option>
                    </select>
                  </td>
                {:else if c.key === 'targets' || c.key === 'comment'}
                  <td class={c.key}><input class="cell" bind:value={it[c.key]} /></td>
                {:else}
                  <td class="editcol"><input class="cell" bind:value={it[c.key]} /></td>
                {/if}
              {/each}
              <td class="act"><button class="delx" title="delete row (atuin untouched)" onclick={() => deleteItems([it])}>✕</button></td>
            </tr>
            {#if it.expanded}
              <tr class="detail">
                <td colspan={cols.length + 2}>
                  <div class="detail-grid">
                    <div class="field">
                      <span class="flabel">
                        Full command
                        <span class="badge" class:ok={it.exit === 0} class:bad={it.exit !== 0}>exit {it.exit}</span>
                      </span>
                      <div class="rawcmd"><span><Highlight text={it.command} q={query} /></span><CopyButton text={it.command} /></div>
                      <div class="drow">
                        <button class="sm ghost" title="add this command as a noise rule" onclick={() => (noiseModal = ruleFor(it.command))}>＋ always noise</button>
                        {#if it.kind === 'merged'}<button class="sm ghost" onclick={() => unmerge(it)}>split merge</button>{/if}
                        <button class="sm danger" onclick={() => deleteItems([it])}>✕ delete row</button>
                      </div>
                    </div>
                    <label class="field">
                      <span class="flabel">Measure (short summary for the report)</span>
                      <textarea class="big" bind:value={it.measure} use:grow></textarea>
                    </label>
                    <label class="field">
                      <span class="flabel">Optional description / comment (defaults to the full command)</span>
                      <textarea class="big" bind:value={it.comment} use:grow></textarea>
                    </label>
                  </div>
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

{#if noiseModal !== null}
  <NoiseRulesModal {rules} prefill={noiseModal} onchange={setRules} onclose={() => (noiseModal = null)} />
{/if}
{#if colsModal}
  <ColumnsModal hidden={hiddenCols} onchange={(h) => setPref('hiddenCols', h)} onclose={() => (colsModal = false)} />
{/if}

<style>
  .tracker { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .scroll { flex: 1; overflow: auto; min-height: 0; }
  .bulk { background: var(--crust); padding: 9px 18px; }
  .search { position: relative; display: flex; align-items: center; }
  .search input { width: 320px; max-width: 60vw; background: var(--base); border-radius: 9px; padding-right: 64px; }
  .hint { position: absolute; right: 9px; color: var(--overlay); font-size: 11px; }
  .kbd { color: var(--faint); font-size: 10px; border: 1px solid var(--surface1); border-radius: 4px; padding: 1px 5px; }
  .muted { color: var(--overlay); font-size: 11px; }
  .link { background: none; border: none; padding: 0; color: var(--accent); text-decoration: underline; font-size: 11px; }
  .pill { background: var(--surface0); border-radius: 20px; padding: 4px 13px; color: var(--subtext); font-size: 12px; }
  .pill b { color: var(--green); font-variant-numeric: tabular-nums; }
  .save { font-size: 11px; color: var(--overlay); min-width: 56px; text-align: right; }
  .save-error { color: var(--red); font-weight: 600; }
  .count { color: var(--accent); font-variant-numeric: tabular-nums; }
  .group {
    display: flex; gap: 6px; align-items: center; background: var(--base);
    border: 1px solid var(--surface1); border-radius: 11px; padding: 5px 9px;
  }
  .glabel { color: var(--overlay); font-size: 10px; text-transform: uppercase; letter-spacing: .6px; font-weight: 600; margin-right: 3px; }
  button.int { border-color: color-mix(in srgb, var(--int) 55%, var(--surface1)); }
  button.int:hover { background: var(--int); color: var(--base); border-color: var(--int); }
  button.ext { border-color: color-mix(in srgb, var(--ext) 55%, var(--surface1)); }
  button.ext:hover { background: var(--ext); color: var(--base); border-color: var(--ext); }

  table { border-collapse: separate; border-spacing: 0; width: 100%; }
  thead th {
    position: sticky; top: 0; background: var(--mantle); z-index: 2; text-align: left; padding: 9px 10px;
    font-size: 11px; color: var(--overlay); font-weight: 600; white-space: nowrap; letter-spacing: .3px;
    text-transform: uppercase; border-bottom: 1px solid var(--surface1);
  }
  tbody td { border-bottom: 1px solid var(--surface0); padding: 0 10px; height: 40px; vertical-align: middle; }
  tr.row:hover { background: var(--mantle); }
  tr.noise { opacity: .42; }
  tr.incl { background: color-mix(in srgb, var(--accent) 11%, transparent); }
  tr.incl:hover { background: color-mix(in srgb, var(--accent) 16%, transparent); }
  tr.incl td:first-child { box-shadow: inset 3px 0 0 var(--accent); }
  .chk { width: 34px; text-align: center; }
  td.num { color: var(--accent); text-align: right; width: 28px; font-weight: 700; font-variant-numeric: tabular-nums; cursor: pointer; }
  td.ro { color: var(--overlay); white-space: nowrap; width: 112px; font-size: 12px; cursor: pointer; user-select: none; font-family: var(--mono); }
  td.measure { max-width: 0; width: 42%; }
  .mcell { display: flex; align-items: center; gap: 6px; }
  .mtext {
    flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: left;
    font-family: var(--mono); font-size: 12.5px; cursor: text; padding: 4px 2px; border: none; background: none; border-radius: 6px;
  }
  .mtext:hover { background: var(--surface0); }
  .mtext.edited { color: var(--cyan); }
  .chev {
    color: var(--overlay); flex: none; width: 20px; height: 20px; padding: 0; display: flex; border: none; background: none;
    align-items: center; justify-content: center; border-radius: 6px; font-size: 11px; opacity: 0;
  }
  tr.row:hover .chev { opacity: 1; }
  .chev.open { transform: rotate(90deg); color: var(--accent); opacity: 1; }
  td.act { width: 34px; text-align: center; }
  .delx { background: transparent; border: 1px solid transparent; color: var(--faint); font-size: 12px; padding: 3px 7px; border-radius: 7px; opacity: .5; }
  tr.row:hover .delx { opacity: 1; color: var(--overlay); }
  .delx:hover { background: var(--red) !important; color: var(--base) !important; border-color: var(--red) !important; }
  input.cell, select.cell {
    width: 100%; background: transparent; border: 1px solid transparent; border-radius: 7px; padding: 5px 6px; color: var(--text);
  }
  input.cell { font-family: var(--mono); font-size: 12px; }
  input.cell:hover, select.cell:hover { border-color: var(--surface1); background: var(--surface0); }
  select.cell { appearance: none; padding-right: 8px; cursor: pointer; }
  select.cell[data-v="Internal"] { color: var(--int); font-weight: 600; }
  select.cell[data-v="External"] { color: var(--ext); font-weight: 600; }
  td.eint { min-width: 96px; }
  td.targets { min-width: 150px; max-width: 220px; }
  td.targets input, td.comment input { font-size: 11.5px; }
  td.editcol { min-width: 82px; }
  td.comment { min-width: 160px; max-width: 240px; }

  tr.detail td { background: var(--crust); padding: 12px 14px 14px 44px; border-bottom: 1px solid var(--surface1); }
  .detail-grid { display: grid; gap: 10px; max-width: 1100px; }
  .field { display: block; }
  .flabel { display: flex; gap: 8px; align-items: center; color: var(--overlay); font-size: 10px; text-transform: uppercase; letter-spacing: .5px; margin-bottom: 4px; font-weight: 600; }
  .rawcmd {
    font-family: var(--mono); font-size: 12.5px; background: var(--base); border: 1px solid var(--surface1);
    border-radius: 8px; padding: 9px 11px; white-space: pre-wrap; word-break: break-word;
    display: flex; justify-content: space-between; gap: 10px; align-items: flex-start;
  }
  .drow { display: flex; gap: 8px; margin-top: 6px; }
  textarea.big { width: 100%; font-family: var(--mono); font-size: 12.5px; background: var(--base); min-height: 56px; resize: vertical; line-height: 1.5; }
  .badge.ok { background: color-mix(in srgb, var(--green) 20%, transparent); color: var(--green); }
  .badge.bad { background: color-mix(in srgb, var(--red) 22%, transparent); color: var(--red); }
</style>
