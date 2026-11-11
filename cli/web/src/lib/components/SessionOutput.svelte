<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { outputPreview } from '../sessions';
  import { app, setPref } from '../app.svelte';
  import { matchPositions } from '../selection';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import Icon from './Icon.svelte';

  let {
    text, expanded = false, onToggle, onFind, query = '', currentMatch = -1, forcedOpen = false,
  }: {
    text: string; expanded?: boolean; onToggle: () => void; onFind: () => void;
    query?: string; currentMatch?: number; forcedOpen?: boolean;
  } = $props();

  const id = $props.id();
  let finding = $state(false);
  let panel: HTMLDivElement | undefined = $state();
  let readingPosition: { node: HTMLElement; top: number } | null = null;
  let search = $state('');
  let index = $state(0);
  let input: HTMLInputElement | undefined = $state();
  let searchButton: HTMLButtonElement | undefined = $state();
  const term = $derived(search.trim().toLowerCase());
  const matches = $derived(matchPositions(text, term));
  const preview = $derived(outputPreview(text));
  const long = $derived(preview.length < text.length);
  const searchExpanded = $derived(finding && matches.length > 0);
  const show = $derived(expanded || forcedOpen || searchExpanded);
  const activeIndex = $derived(matches.length ? index % matches.length : 0);

  async function navigate(delta: number) {
    if (!matches.length) return;
    index = (activeIndex + delta + matches.length) % matches.length;
    await tick();
    document.getElementById(`${id}-match`)?.scrollIntoView({ block: 'nearest' });
  }

  $effect(() => {
    term;
    untrack(() => { index = 0; if (finding && term) navigate(0); });
  });

  $effect(() => {
    query; currentMatch;
    untrack(() => { finding = false; search = ''; index = 0; });
  });

  async function openSearch() {
    const node = panel?.closest<HTMLElement>('[aria-label="Recorded commands and output"]');
    readingPosition = node ? { node, top: node.scrollTop } : null;
    onFind();
    finding = true;
    await tick();
    input?.focus();
  }

  async function closeSearch() {
    finding = false;
    search = '';
    index = 0;
    await tick();
    if (readingPosition) readingPosition.node.scrollTop = readingPosition.top;
    searchButton?.focus({ preventScroll: true });
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); navigate(e.shiftKey ? -1 : 1); }
    if (e.key === 'Escape') { e.preventDefault(); closeSearch(); }
  }
</script>

<div class="output-panel" bind:this={panel} class:finding class:nowrap={app.prefs.outputWrap === false}>
  <div class="output-actions"><span class="output-label">Output</span><span class="grow"></span><button class="sm ghost wrap" aria-label="Wrap output" title="Wrap output" aria-pressed={app.prefs.outputWrap !== false} onclick={() => setPref('outputWrap', app.prefs.outputWrap === false)}>Wrap</button>
    <button class="sm ghost output-search" bind:this={searchButton} title="Find in output" aria-label="Find in output" aria-expanded={finding} aria-controls={`${id}-find`} onclick={() => finding ? closeSearch() : openSearch()}><Icon name="search" size={14} /></button>
    <CopyButton text={text} title="Copy full output" />
  </div>
  {#if finding}
    <div class="output-find" id={`${id}-find`} role="search" aria-label="Find in command output">
      <Icon name="search" size={14} />
      <input bind:this={input} bind:value={search} onkeydown={keydown} aria-label="Search this output" placeholder="Find in output…" />
      {#if term}<span class="counter" role="status">{matches.length ? `${activeIndex + 1} / ${matches.length}` : '0 matches'}</span>{/if}
      <button class="find-action" title="Previous match (Shift+Enter)" aria-label="Previous output match" disabled={!matches.length} onclick={() => navigate(-1)}>↑</button>
      <button class="find-action" title="Next match (Enter)" aria-label="Next output match" disabled={!matches.length} onclick={() => navigate(1)}>↓</button>
      <button class="find-action" title="Close output search (Escape)" aria-label="Close output search" onclick={closeSearch}>×</button>
    </div>
  {/if}
  <pre class="output"><Highlight text={show ? text : preview} q={finding ? search : query} curStart={finding ? (matches[activeIndex] ?? -1) : currentMatch} curId={finding ? `${id}-match` : 'find-cur'} /></pre>
</div>
{#if long}
  <button class="output-toggle" aria-expanded={!!show} disabled={forcedOpen || searchExpanded} onclick={onToggle}>{forcedOpen || searchExpanded ? 'Full output shown for search match' : show ? 'Collapse output' : `Show full output · ${(text.length - preview.length).toLocaleString()} characters hidden`}</button>
{/if}

<style>
  .output-panel { min-width: 0; background: var(--pd-terminal); margin-top: 8px; }
  .output-actions { display: flex; align-items: center; gap: 4px; color: var(--pd-text-muted); }.output-label { font-size: .75rem; }.wrap[aria-pressed=true] { color: var(--pd-text); background: var(--pd-surface); }
  .output-search { display: inline-flex; align-items: center; justify-content: center; min-width: 32px; padding: 4px; }.output-search[aria-expanded=true] { color: var(--pd-accent); }
  .output { min-width: 0; font: .8125rem/1.5 var(--pd-mono); white-space: pre-wrap; overflow-wrap: anywhere; color: var(--pd-text); margin: 4px 0 0; padding: 4px 0; tab-size: 8; overflow-x: auto; }.nowrap .output { white-space: pre; overflow-wrap: normal; }
  .output-find { display: flex; align-items: center; gap: 4px; min-width: 0; border: 1px solid var(--pd-control-border); border-radius: 6px; background: var(--pd-surface); padding: 0 6px; margin: 8px 0; color: var(--pd-text-muted); }.output-find:focus-within { outline: 2px solid var(--pd-accent); outline-offset: 2px; }.output-find input { flex: 1; width: 0; min-width: 24px; padding: 6px; background: transparent; border: 0; }.output-find input:focus-visible { outline: none; }
  .counter { font-size: .75rem; font-variant-numeric: tabular-nums; white-space: nowrap; }.find-action { border-color: transparent; background: transparent; padding: 4px; min-width: 28px; }.output-toggle { margin-top: 8px; padding: 4px 0; min-height: 32px; background: transparent; border: 0; font-size: .8125rem; color: var(--pd-accent); }.output-toggle:disabled { opacity: 1; color: var(--pd-text-muted); }
</style>
