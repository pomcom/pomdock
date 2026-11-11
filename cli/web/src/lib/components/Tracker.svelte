<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api';
  import { app, copyText, setPref, toast, registerTrackerFlush } from '../app.svelte';
  import {
    activeColumns, buildItems, fuzzyScore, mergeItems, noiseMatcher, rowItem, ruleFor, serialize, toTSV,
    type Defaults, type Fields, type Item, type SavedState,
  } from '../tracker';
  import type { ReportRow } from '../types';
  import ColumnsModal from './ColumnsModal.svelte';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import NoiseRulesModal from './NoiseRulesModal.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import Icon from './Icon.svelte';
  import { toggleSelection, toggleVisible } from '../selection';

  // --- data + persistence ------------------------------------------------------

  let rowsById = new Map<string, ReportRow>();
  let items: Item[] = $state([]);
  let loading = $state(false);
  let error = $state('');
  let saveStatus: '' | 'saved' | 'saving' | 'error' = $state('');
  let loadedFor = '';
  let loadVersion = 0;
  let lastSaved = '';
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  let pendingSave: (() => Promise<void>) | null = null;
  let inFlight: Promise<void> | null = null;
  let saveFailure: Error | null = null;
  registerTrackerFlush(flushSave);

  let savedDefaults: Defaults | null = $state(null);
  const defaults: Defaults = $derived(savedDefaults ?? {
    tester: app.prefs.tester || app.meta?.tester || 'LS',
    source: app.prefs.source ?? '',
  });

  $effect(() => {
    const name = app.engagement;
    if (name) untrack(() => load(name));
  });

  async function load(name: string) {
    const version = ++loadVersion;
    if (loadedFor !== name) { selected = new Set(); query = ''; }
    loading = true;
    error = '';
    try {
      await flushSave();
      const [rows, saved] = await Promise.all([api.rows(name), api.trackerState<SavedState>(name)]);
      if (version !== loadVersion || name !== app.engagement) return; // switched away meanwhile
      rowsById = new Map(rows.map((r) => [r.id, r]));
      savedDefaults = saved?.defaults ?? null;
      bulkSource = defaults.source;
      items = buildItems(rows, saved?.version === 1 ? saved : null, defaults);
      lastSaved = JSON.stringify(serialize(items, rowsById, defaults));
      loadedFor = name;
      saveStatus = 'saved';
      lastClicked = null;
    } catch (e) {
      if (version !== loadVersion) return;
      error = (e as Error).message;
      items = [];
      loadedFor = '';
    } finally {
      if (version === loadVersion) loading = false;
    }
  }

  // Every edit is saved (debounced) to <loot>/.pomdock/tracker.json.
  $effect(() => {
    const json = JSON.stringify(serialize(items, rowsById, defaults));
    if (!loadedFor || json === lastSaved) return;
    const name = loadedFor;
    saveStatus = 'saving';
    clearTimeout(saveTimer);
    const save = async () => {
      try {
        await api.saveTrackerState(name, JSON.parse(json));
        lastSaved = json;
        saveFailure = null;
        if (!pendingSave) saveStatus = 'saved';
      } catch (e) {
        saveFailure = e as Error;
        saveStatus = 'error';
        toast(`saving the tracker failed: ${(e as Error).message}`);
        if (!pendingSave) pendingSave = save;
        throw e;
      }
    };
    pendingSave = save;
    saveTimer = setTimeout(() => { void flushSave().catch(() => {}); }, 600);
  });

  async function flushSave() {
    clearTimeout(saveTimer);
    while (inFlight || pendingSave) {
      if (inFlight) { await inFlight; continue; }
      const save = pendingSave;
      if (!save) continue;
      pendingSave = null;
      const task = save();
      inFlight = task;
      try { await task; } finally { if (inFlight === task) inFlight = null; }
    }
    if (saveFailure) throw saveFailure;
  }

  function onbeforeunload(e: BeforeUnloadEvent) {
    if (saveStatus === 'saving') {
      void flushSave().catch(() => {});
      e.preventDefault();
    }
  }

  // --- view state --------------------------------------------------------------

  let query = $state('');
  let showNoise = $state(false);
  let lastClicked: string | null = null;
  let selected: Set<string> = $state(new Set());
  let sortField = $state('started');
  let ascending = $state(true);
  let resizeColumn: { key: string; x: number; width: number } | null = null;
  const selectedRows = $derived(items.filter(it => selected.has(it.key) && !it.deleted));
  const DISPLAY_ORDER = ['num', 'measure', 'targets', 'outcome', 'started', 'ended', 'ie', 'source', 'tester', 'mitre', 'comment'];
  const widths: Record<string, number> = { num: 48, measure: 344, targets: 168, outcome: 128, started: 176, ended: 176, ie: 112, source: 152, tester: 88, mitre: 112, comment: 360 };
  const columnMin = (key: string) => key === 'num' ? 40 : 64;
  const columnWidth = (key: string) => Math.max(columnMin(key), Math.min(640, app.prefs.trackerWidths?.[key] ?? widths[key] ?? 144));
  let noiseModal: string | null = $state(null); // prefill text while open
  let colsModal = $state(false);
  let searchEl: HTMLInputElement | undefined = $state();

  const hiddenCols = $derived(app.prefs.hiddenCols ?? ['mitre']);
  const cols = $derived(activeColumns(hiddenCols));
  const displayCols = $derived([...cols].sort((a, b) => DISPLAY_ORDER.indexOf(a.key) - DISPLAY_ORDER.indexOf(b.key)));
  const tableWidth = $derived(displayCols.reduce((sum, col) => sum + columnWidth(col.key), 136));
  const rules = $derived(app.prefs.noiseRules ?? []);
  const isRuleNoise = $derived(noiseMatcher(rules));
  const isNoise = (it: Item) => it.noise || isRuleNoise(it.command);

  const visible = $derived.by(() => {
    const q = query.trim();
    const base = items
      .filter((it) => !it.deleted)
      .filter((it) => showNoise || it.incl || !isNoise(it));
    if (!q) return base.sort((a, b) => (sortField === 'started' || sortField === 'num' ? a.ts - b.ts : sortField === 'ended' ? a.endTs - b.endTs : String(sortField === 'measure' ? a.measure || a.command : a[sortField as keyof Item] ?? '').localeCompare(String(sortField === 'measure' ? b.measure || b.command : b[sortField as keyof Item] ?? ''))) * (ascending ? 1 : -1));
    // Fuzzy (fzf-style) match over command + measure + targets, best first.
    return base
      .map((it) => ({ it, score: fuzzyScore(`${it.command} ${it.measure} ${it.targets}`, q) }))
      .filter((x) => x.score >= 0)
      .sort((a, b) => b.score - a.score || a.it.ts - b.it.ts)
      .map((x) => x.it);
  });
  const included = $derived(items.filter((it) => it.incl && !it.deleted));
  const numbers = $derived(new Map([...included].sort((a, b) => a.ts - b.ts).map((it, i) => [it.key, i + 1])));
  const noiseCount = $derived(items.filter((it) => !it.deleted && isNoise(it)).length);
  const deletedCount = $derived(items.filter((it) => it.deleted).length);
  const allVisibleChecked = $derived(visible.length > 0 && visible.every((it) => it.incl));

  // --- actions -----------------------------------------------------------------

  function toggleRow(idx: number, shift: boolean) {
    const it = visible[idx]; if (!it) return;
    selected = toggleSelection(visible.map(row => row.key), selected, it.key, lastClicked, shift);
    lastClicked = it.key;
  }
  function includeSelected(incl: boolean) { for (const it of selectedRows) it.incl = incl; }
  function sort(key: string) { if (sortField === key) ascending = !ascending; else { sortField = key; ascending = true; } }
  function setWidth(key: string, width: number) { setPref('trackerWidths', { ...app.prefs.trackerWidths, [key]: Math.max(columnMin(key), Math.min(640, width)) }); }
  function resizeKey(e: KeyboardEvent, key: string) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault(); setWidth(key, e.key === 'Home' ? columnMin(key) : e.key === 'End' ? 640 : columnWidth(key) + (e.key === 'ArrowLeft' ? -16 : 16));
  }
  function deleteItems(list: Item[]) {
    if (!list.length) return toast('nothing checked');
    for (const it of list) it.deleted = true;
    selected = new Set([...selected].filter(key => !list.some(it => it.key === key)));
    toast(`removed ${list.length} row${list.length > 1 ? 's' : ''} (atuin untouched)`, () => {
      for (const it of list) it.deleted = false;
    });
  }

  function restoreAll() {
    for (const it of items) it.deleted = false;
    toast('restored');
  }

  function bulkSet(field: keyof Fields, value: string) {
    if (!selectedRows.length) return toast('Select rows first');
    for (const it of selectedRows) (it as Record<string, unknown>)[field] = value;
    toast(`${selectedRows.length} rows → ${field}${value ? `: ${value}` : ' cleared'}`);
  }

  function setReportDefault(field: 'source' | 'tester', value: string) {
    savedDefaults = { ...defaults, [field]: value };
    setPref(field, value);
  }

  function merge() {
    if (selectedRows.length < 2) return toast('Select 2+ rows to merge');
    const merged = mergeItems(selectedRows, defaults);
    const gone = new Set(selectedRows.map(it => it.key));
    items = [...items.filter((it) => !gone.has(it.key)), merged];
    lastClicked = null; selected = new Set([merged.key]);
    toast(`merged ${gone.size} rows`);
  }

  function unmerge(m: Item) {
    const restored = m.ids.map((id) => rowsById.get(id)).filter((r) => r !== undefined)
      .map((r) => rowItem(r, defaults, m.sourceOverrides?.[r.id] ?? { incl: m.incl }));
    items = [...items.filter((it) => it.key !== m.key), ...restored];
    selected = new Set(restored.map(it => it.key));
    toast(`split back into ${restored.length} rows`);
  }

  function setRules(next: string[]) {
    setPref('noiseRules', next);
  }

  function markCheckedNoise() {
    if (!selectedRows.length) return toast('Select rows first');
    const next = [...rules];
    for (const it of selectedRows) {
      const r = ruleFor(it.command);
      if (r && !next.includes(r)) next.push(r);
    }
    const added = next.length - rules.length;
    setRules(next);
    toast(`Added ${added} noise rules`);
  }

  function tsv() {
    return toTSV(items, cols, !!app.prefs.withHeader);
  }

  async function copyTSV() {
    const text = tsv();
    if (!text) return toast('nothing checked');
    try { await copyText(text); toast(`Copied ${included.length} included rows`); } catch (e) { toast((e as Error).message); }
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
    if (e.ctrlKey || e.metaKey || e.altKey || (el instanceof HTMLElement && (el.isContentEditable || el.closest('[role=dialog], [role=menu], .xterm, [data-terminal]')))) return;
    if (e.key === '/' && !(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement)) {
      e.preventDefault();
      searchEl?.focus();
    }
    if (e.key === 'Escape' && el === searchEl) {
      query = '';
      searchEl?.blur();
    }
  }

  $effect(() => { const focus = () => searchEl?.focus(); window.addEventListener('pomdock:search', focus); return () => window.removeEventListener('pomdock:search', focus); });

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
  <div class="tracker-tools">
  <div class="bar utilities" aria-label="Tracker table actions">
    <div class="search"><Icon name="search" /><input bind:this={searchEl} aria-label="Search tracker commands, measures and targets" title="Fuzzy match across commands, measures and targets" bind:value={query} placeholder="Search commands, measures, targets…" />{#if query}<span class="hint">{visible.length}</span>{:else}<kbd>/</kbd>{/if}</div>
    <div class="view-options" role="group" aria-label="Table view options">
    <label class="check-label"><input type="checkbox" bind:checked={showNoise} /> Show noise ({noiseCount})</label>
    <button class="sm ghost" onclick={() => colsModal = true}>Columns</button><button class="sm ghost" onclick={() => noiseModal = ''}>Noise rules</button>
    <button class="sm ghost" aria-pressed={!app.prefs.bulkHidden} onclick={() => setPref('bulkHidden', !app.prefs.bulkHidden)}>{app.prefs.bulkHidden ? 'Show' : 'Hide'} edit bar</button>
    <button class="sm ghost" aria-label="Refresh tracker" onclick={() => load(app.engagement)}><Icon name="refresh" /></button>
    </div>
    <div class="export-actions" role="group" aria-label="Export actions"><span class="save save-{saveStatus}" role="status">{saveStatus === 'saving' ? 'Saving…' : saveStatus === 'error' ? 'Save failed' : saveStatus === 'saved' ? 'Saved locally' : ''}</span>
    <button class="accent" disabled={!included.length} onclick={copyTSV}>Copy TSV</button>
    <ActionMenu label="Export options"><button role="menuitem" disabled={!included.length} onclick={download}>Download .tsv</button><button role="menuitemcheckbox" aria-checked={!!app.prefs.withHeader} onclick={() => setPref('withHeader', !app.prefs.withHeader)}>{app.prefs.withHeader ? '✓ ' : ''}Include header row</button></ActionMenu>
    </div>
  </div>
  <div class="export-scope"><span><b>{included.length}</b> included in export · all included rows, regardless of selection or search · sheet columns · time order</span>{#if deletedCount}<button class="sm ghost" onclick={restoreAll}>Restore {deletedCount} deleted</button>{/if}</div>
  {#if selectedRows.length}
    <div class="bar selection" aria-label="Selected tracker row actions"><strong>{selectedRows.length} selected</strong>{#if selectedRows.some(it => !visible.includes(it))}<span class="muted">{selectedRows.filter(it => !visible.includes(it)).length} outside this filter</span>{/if}<button class="sm" onclick={() => includeSelected(true)}>Include in export</button><button class="sm" onclick={() => includeSelected(false)}>Exclude from export</button><button class="sm ghost" onclick={() => selected = new Set()}>Clear selection</button><span class="grow"></span><button class="sm" disabled={selectedRows.length < 2} title="Includes the merged row only when all its source rows are included" onclick={merge}>Merge selected</button><button class="sm ghost" onclick={markCheckedNoise}>Mark noise</button><button class="sm danger" onclick={() => deleteItems(selectedRows)}>Delete selected</button></div>
    {#if !app.prefs.bulkHidden}
      <div class="bar bulk" aria-label="Bulk edit selected tracker rows">
        <div class="group"><span class="glabel">Ext/Int</span><button class="sm" onclick={() => bulkSet('ie', 'Internal')}>Internal</button><button class="sm" onclick={() => bulkSet('ie', 'External')}>External</button><button class="sm ghost" onclick={() => bulkSet('ie', '')}>Clear</button></div>
        <div class="group"><label for="bulk-source">Source</label><input id="bulk-source" bind:value={bulkSource} placeholder="192.0.2.5" size="12" onkeydown={(e) => e.key === 'Enter' && (bulkSet('source', bulkSource), setReportDefault('source', bulkSource))} /><button class="sm" title="Apply to selected rows and use as the default for new rows" onclick={() => { bulkSet('source', bulkSource); setReportDefault('source', bulkSource); }}>Set + default</button></div>
        {#if !hiddenCols.includes('tester')}<div class="group"><label for="bulk-tester">Tester</label><input id="bulk-tester" bind:value={bulkTester} placeholder={defaults.tester} size="5" /><button class="sm" title="Apply to selected rows and use as the default for new rows" onclick={() => { const v = bulkTester || defaults.tester; bulkSet('tester', v); setReportDefault('tester', v); }}>Set + default</button></div>{/if}
        {#if !hiddenCols.includes('mitre')}<div class="group"><label for="bulk-mitre">MITRE</label><input id="bulk-mitre" bind:value={bulkMitre} placeholder="T1552.001" size="10" /><button class="sm" onclick={() => bulkSet('mitre', bulkMitre)}>Set</button></div>{/if}
        <div class="group"><span class="glabel">Outcome</span><button class="sm" onclick={() => bulkSet('outcome', 'Successful')}>Successful</button><button class="sm" onclick={() => bulkSet('outcome', 'Failed')}>Failed</button><button class="sm ghost" onclick={() => bulkSet('outcome', '')}>Clear</button></div>
      </div>
    {/if}
  {/if}

  </div>
  <!-- Focusable table region provides keyboard horizontal scrolling. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="scroll" role="region" aria-label="Tracker records" tabindex="0" aria-busy={loading}>
    {#if error}
      <div class="empty" role="alert">Could not load Tracker: {error}<p><button onclick={() => load(app.engagement)}>Retry</button></p></div>
    {:else if loading && !items.length}
      <div class="empty">loading…</div>
    {:else if app.engagements.find(e => e.name === app.engagement)?.archived}
      <div class="empty">This engagement is archived. <a href={`#/sessions?name=${encodeURIComponent(app.engagement)}`}>Restore it from Session history</a>.</div>
    {:else if !app.engagement}
      <div class="empty">No engagement with an atuin history found yet. Open a shell with <code>pomdock docker exec --name NAME</code>.</div>
    {:else if !visible.length}
      <div class="empty">No rows match. Clear the search or turn on “show noise”.</div>
    {:else}
      <table style:width={`${tableWidth}px`}>
        <caption class="sr-only">Engagement activity tracker. Select rows to edit; Export checkboxes control TSV inclusion separately.</caption>
        <colgroup><col style:width="40px" /><col style:width="56px" />{#each displayCols as c}<col style:width={`${columnWidth(c.key)}px`} />{/each}<col style:width="40px" /></colgroup>
        <thead>
          <tr>
            <th class="chk" scope="col"><input type="checkbox" aria-label="Select all visible rows" checked={visible.length > 0 && visible.every(it => selected.has(it.key))} indeterminate={visible.some(it => selected.has(it.key)) && !visible.every(it => selected.has(it.key))} onchange={() => selected = toggleVisible(visible.map(it => it.key), selected)} /></th>
            <th class="export-check" scope="col"><span>Export</span><input type="checkbox" aria-label="Include all visible rows in export" checked={allVisibleChecked} indeterminate={visible.some(it => it.incl) && !allVisibleChecked} onchange={(e) => { for (const it of visible) it.incl = e.currentTarget.checked; }} /></th>
            {#each displayCols as c (c.key)}<th scope="col" aria-sort={sortField === c.key ? ascending ? 'ascending' : 'descending' : 'none'}><button class="sort" onclick={() => sort(c.key)}>{c.short}{#if sortField === c.key}<span aria-hidden="true">{ascending ? ' ↑' : ' ↓'}</span>{/if}</button><!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions --><span class="column-resize" role="separator" aria-label={`Resize ${c.short} column`} aria-orientation="vertical" aria-valuemin={columnMin(c.key)} aria-valuemax={640} aria-valuenow={columnWidth(c.key)} tabindex="0" onkeydown={(e) => resizeKey(e, c.key)} onpointerdown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); resizeColumn = { key: c.key, x: e.clientX, width: columnWidth(c.key) }; }} onpointermove={(e) => { if (resizeColumn?.key === c.key) setWidth(c.key, resizeColumn.width + e.clientX - resizeColumn.x); }} onpointerup={() => resizeColumn = null} onlostpointercapture={() => resizeColumn = null}></span></th>{/each}<th scope="col"><span class="sr-only">Row actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each visible as it, idx (it.key)}
            <tr class="row" class:alternate={idx % 2 === 1} class:selected={selected.has(it.key)} class:incl={it.incl} class:noise={isNoise(it)}>
              <td class="chk"><input type="checkbox" aria-label={`Select row ${idx + 1}`} checked={selected.has(it.key)} onclick={(e) => toggleRow(idx, e.shiftKey)} /></td><td class="export-check"><input type="checkbox" aria-label={`Include row ${idx + 1} in export`} bind:checked={it.incl} /></td>
              {#each displayCols as c (c.key)}
                {#if c.key === 'num'}
                  <td class="num">{it.incl ? numbers.get(it.key) : ''}</td>
                {:else if c.key === 'started' || c.key === 'ended'}
                  <td class="ro" title={String(it[c.key])}>{it[c.key]}</td>
                {:else if c.key === 'measure'}
                  <td class="measure">
                    <div class="mcell">
                      <button class="chev" class:open={it.expanded} title="Expand or collapse command" aria-label="Expand or collapse command" aria-expanded={it.expanded} onclick={() => (it.expanded = !it.expanded)}><Icon name="chevron-down" /></button>
                      <input class="cell measure-input" class:empty-measure={!it.measure} aria-label={`Measure for row ${idx + 1}`} bind:value={it.measure} placeholder={`Add measure · ${it.command}`} title={`Original command: ${it.command}`} />
                      {#if it.kind === 'merged'}<span class="badge" title={`${it.ids.length} merged source rows`}>{it.ids.length}</span>{/if}
                    </div>
                  </td>
                {:else if c.key === 'ie'}
                  <td class="eint">
                    <select class="cell" aria-label={`Internal or external for row ${idx + 1}`} data-v={it.ie} bind:value={it.ie}>
                      <option value="">—</option><option>Internal</option><option>External</option>
                    </select>
                  </td>
                {:else if c.key === 'comment'}
                  <td class="comment"><input class="cell" class:technical={it.comment === it.command} aria-label={`${c.header} for row ${idx + 1}`} bind:value={it.comment} title={it.comment} /></td>
                {:else if c.key === 'targets'}
                  <td class={c.key}><input class="cell technical" aria-label={`${c.header} for row ${idx + 1}`} bind:value={it[c.key]} title={String(it[c.key])} /></td>
                {:else}
                  <td class="editcol"><input class="cell" class:technical={c.key === 'source' || c.key === 'mitre'} aria-label={`${c.header} for row ${idx + 1}`} bind:value={it[c.key]} title={String(it[c.key])} /></td>
                {/if}
              {/each}
              <td class="act"><button class="delx" title="Delete report row (history preserved)" aria-label="Delete report row" onclick={() => deleteItems([it])}><Icon name="trash" /></button></td>
            </tr>
            {#if it.expanded}
              <tr class="detail">
                <td colspan={displayCols.length + 3}>
                  <div class="detail-grid">
                    <div class="field">
                      <span class="flabel">
                        Full command
                        {#if it.exit !== null && it.exit < 0}<span class="badge">Exit not captured</span>{:else if it.exit !== null}<span class="badge" class:ok={it.exit === 0} class:bad={it.exit !== 0}>Exit {it.exit}</span>{:else}<span class="badge">Combined rows · no single exit code</span>{/if}
                      </span>
                      <div class="rawcmd"><span><Highlight text={it.command} q={query} fuzzy /></span><CopyButton text={it.command} title="Copy command" /></div>
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
  .tracker { --pd-control-height: 32px; --pd-row-height: 40px; display: flex; flex-direction: column; height: 100%; min-height: 0; }
  :global([data-density=compact]) .tracker { --pd-control-height: 30px; --pd-row-height: 36px; }
  .tracker-tools { flex: none; max-height: 55%; overflow: auto; }
  .scroll { flex: 1; overflow: auto; min-height: 0; background: var(--pd-sheet); }
  .utilities { gap: 8px 16px; padding: 10px 16px; background: var(--pd-bg); border-bottom-color: var(--pd-sheet-divider); }
  .search { display: flex; align-items: center; gap: 8px; flex: 1 1 280px; min-width: min(240px, 100%); max-width: 360px; padding: 0 8px; border: 1px solid var(--pd-control-border); border-radius: 6px; background: var(--pd-field); color: var(--pd-text-muted); }
  .search input { flex: 1; width: 0; min-width: 40px; border: 0; background: transparent; padding: 4px 0; }
  .search:focus-within { outline: 2px solid var(--pd-accent); outline-offset: 2px; }.search input:focus-visible { outline: 0; }
  .hint { font-size: .75rem; font-variant-numeric: tabular-nums; }.check-label { display: flex; align-items: center; gap: 8px; white-space: nowrap; }
  .view-options, .export-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; }.view-options { gap: 4px 8px; }.export-actions { margin-left: auto; gap: 8px; }
  .export-scope { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; padding: 6px 16px; gap: 8px; color: var(--pd-text-muted); font-size: .75rem; border-bottom: 1px solid var(--pd-sheet-divider); background: var(--pd-bg); }.export-scope b { color: var(--pd-text); font-variant-numeric: tabular-nums; }
  .save, .muted { font-size: .75rem; color: var(--pd-text-muted); }.save-error { color: var(--pd-danger); }
  .selection { background: var(--pd-sidebar); border-bottom-color: var(--pd-sheet-divider); box-shadow: inset 2px 0 var(--pd-accent); }.selection strong { font-size: .875rem; }
  .bulk { background: var(--pd-bg); border-bottom-color: var(--pd-sheet-divider); gap: 8px 20px; }.group { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 4px 0; }.group input { max-width: 160px; background: var(--pd-field); }.glabel { color: var(--pd-text-muted); font-size: .8125rem; }
  table { border-collapse: separate; border-spacing: 0; table-layout: fixed; font-size: .875rem; background: var(--pd-sheet); }
  thead th { position: sticky; top: 0; z-index: 2; padding: 4px 8px; background: var(--pd-sidebar); border-bottom: 1px solid var(--pd-control-border); text-align: left; font-size: .75rem; font-weight: 500; color: var(--pd-text-muted); }
  .sort { display: block; width: 100%; text-align: left; padding: 4px; border: 0; background: transparent; font-size: .8125rem; color: var(--pd-text-muted); }
  .column-resize { position: absolute; right: 0; top: 0; bottom: 0; width: 8px; cursor: col-resize; touch-action: none; }.column-resize:hover { background: var(--pd-control-border); }
  td { border-bottom: 1px solid var(--pd-sheet-divider); height: var(--pd-row-height); padding: 3px 8px; vertical-align: middle; }
  tr.row { background: var(--pd-sheet); }tr.row.alternate { background: var(--pd-sheet-alt); }tr.row:hover { background: var(--pd-sheet-hover); }
  tr.row.selected { background: var(--pd-selected); }tr.row:focus-within { --pd-control-border: var(--pd-text-muted); background: var(--pd-sheet-edit); }
  tr.selected td:first-child { box-shadow: inset 2px 0 var(--pd-accent); }tr.incl .export-check { color: var(--pd-accent); }
  .chk, .export-check { text-align: center; }.chk input, .export-check input { vertical-align: middle; }.export-check span { display: block; font-size: .75rem; margin-bottom: 4px; }
  td.num { font-variant-numeric: tabular-nums; text-align: right; color: var(--pd-text-muted); }.ro { font: .8125rem/1.5 var(--pd-ui); font-variant-numeric: tabular-nums; color: var(--pd-text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .mcell { display: flex; align-items: center; gap: 4px; min-width: 0; }.measure-input { flex: 1; min-width: 0; }.measure-input.empty-measure::placeholder { font-family: var(--pd-mono); font-size: .8125rem; }
  .chev, .delx { display: inline-flex; align-items: center; justify-content: center; flex: none; min-width: 28px; width: 28px; height: var(--pd-control-height); padding: 4px; background: transparent; border: 1px solid transparent; color: var(--pd-text-muted); border-radius: 4px; }
  .chev :global(svg) { transform: rotate(-90deg); }.chev.open :global(svg) { transform: rotate(0); }.delx:hover { color: var(--pd-danger); background: var(--pd-hover); }.act { text-align: center; }
  .cell { width: 100%; min-height: var(--pd-control-height); height: auto; font: .875rem/1.25rem var(--pd-ui); color: var(--pd-text); background: var(--pd-field); border: 1px solid var(--pd-field-border); border-bottom-color: var(--pd-control-border); border-radius: 5px; padding: 4px 7px; }
  .cell.technical { font-family: var(--pd-mono); font-size: .8125rem; }
  .cell:hover { border-color: var(--pd-control-border); }
  .cell:focus { background: var(--pd-field); border-color: var(--pd-accent); outline: 2px solid var(--pd-accent); outline-offset: 1px; }
  .cell:global([aria-invalid=true]), .cell:invalid { border-color: var(--pd-danger); outline-color: var(--pd-danger); }
  tr.detail td { background: var(--pd-bg); padding: 16px; }.detail-grid { display: grid; gap: 16px; max-width: 1000px; }.field { display: grid; gap: 8px; }.flabel { display: flex; align-items: center; gap: 8px; color: var(--pd-text-muted); font-size: .8125rem; }
  .rawcmd { display: flex; align-items: start; justify-content: space-between; gap: 8px; padding: 12px; border: 1px solid var(--pd-divider); border-radius: 6px; background: var(--pd-terminal); font: .8125rem/1.5 var(--pd-mono); white-space: pre-wrap; overflow-wrap: anywhere; }.rawcmd span { min-width: 0; }.drow { display: flex; gap: 8px; flex-wrap: wrap; }textarea.big { width: 100%; resize: vertical; min-height: 56px; font: .875rem/1.5 var(--pd-ui); }.badge.ok { color: var(--pd-success); }.badge.bad { color: var(--pd-danger); }
  @media (max-width: 600px) { .bar, .utilities { padding-inline: 8px; }.utilities { gap: 8px; }.search { max-width: none; flex-basis: 100%; }.view-options { gap: 4px; }.export-actions { flex-wrap: wrap; }.export-scope { padding-inline: 8px; }.group { gap: 4px; }.group input { max-width: 140px; } }
</style>
