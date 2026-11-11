<script lang="ts">
  import { tick } from 'svelte';
  import type { Engagement } from '../types';
  import Icon from './Icon.svelte';

  let { engagements, value, onchange, disabled = false }: {
    engagements: Engagement[]; value: string; onchange: (name: string) => void; disabled?: boolean;
  } = $props();

  const id = $props.id();
  let open = $state(false);
  let query = $state('');
  let index = $state(0);
  let root: HTMLDivElement | undefined = $state();
  let trigger: HTMLButtonElement | undefined = $state();
  let input: HTMLInputElement | undefined = $state();
  let left = $state(0);
  let top = $state(0);
  let width = $state(280);
  let height = $state(320);
  const filtered = $derived(engagements.filter(e => e.name.toLowerCase().includes(query.trim().toLowerCase())));
  const activeIndex = $derived(Math.min(index, Math.max(0, filtered.length - 1)));
  const active = $derived(filtered[activeIndex]);

  function position() {
    if (!open || !trigger) return;
    const rect = trigger.getBoundingClientRect();
    width = Math.min(300, window.innerWidth - 24);
    left = Math.max(12, Math.min(rect.left, window.innerWidth - width - 12));
    top = rect.bottom + 6;
    height = Math.max(80, Math.min(320, window.innerHeight - top - 12));
  }

  async function show(initialQuery = '', last = false) {
    query = initialQuery;
    index = initialQuery ? 0 : last ? engagements.length - 1 : Math.max(0, engagements.findIndex(e => e.name === value));
    open = true;
    position();
    await tick();
    input?.focus();
    scrollActive();
  }

  function close(restoreFocus = false) {
    open = false;
    query = '';
    if (restoreFocus) trigger?.focus();
  }

  function choose(name: string) {
    onchange(name);
    close(true);
  }

  function scrollActive() {
    document.getElementById(`${id}-option-${activeIndex}`)?.scrollIntoView({ block: 'nearest' });
  }

  async function move(delta: number) {
    if (!filtered.length) return;
    index = (activeIndex + delta + filtered.length) % filtered.length;
    await tick();
    scrollActive();
  }

  function triggerKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      show('', e.key === 'ArrowUp');
    } else if (e.key.length === 1 && e.key !== ' ' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      show(e.key);
    }
  }

  function searchKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      move(e.key === 'ArrowDown' ? 1 : -1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (active) choose(active.name);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close(true);
    } else if (e.key === 'Tab' && e.shiftKey) {
      e.preventDefault();
      close(true);
    }
  }

  function outside(e: PointerEvent) {
    if (open && e.target instanceof Node && !root?.contains(e.target)) close();
  }

  function focusout(e: FocusEvent) {
    if (open && !(e.relatedTarget instanceof Node && root?.contains(e.relatedTarget))) close();
  }
</script>

<svelte:window onpointerdown={outside} onresize={position} onscroll={position} />

<div class="picker" bind:this={root} onfocusout={focusout}>
  <button type="button" class="trigger" bind:this={trigger} aria-label="Select engagement" aria-haspopup="listbox" aria-expanded={open} aria-controls={`${id}-list`} disabled={disabled || !engagements.length} title={value || 'No engagements'} onkeydown={triggerKeydown} onclick={() => open ? close(true) : show()}>
    <span class="value">{value || 'No engagements'}</span><Icon name="chevron-down" size={14} />
  </button>
  {#if open}
    <div class="menu" style:left={`${left}px`} style:top={`${top}px`} style:width={`${width}px`} style:max-height={`${height}px`}>
      <div class="search"><Icon name="search" size={14} /><input bind:this={input} bind:value={query} oninput={() => index = 0} onkeydown={searchKeydown} role="combobox" aria-label="Search engagements" aria-autocomplete="list" aria-expanded="true" aria-controls={`${id}-list`} aria-activedescendant={active ? `${id}-option-${activeIndex}` : undefined} autocomplete="off" spellcheck="false" placeholder="Find engagement…" /></div>
      <div class="options" id={`${id}-list`} role="listbox" aria-label="Engagements" tabindex="-1">
        {#each filtered as engagement, i (engagement.name)}
          <button type="button" class="option" class:active={i === activeIndex} class:selected={engagement.name === value} id={`${id}-option-${i}`} role="option" aria-selected={engagement.name === value} tabindex="-1" title={engagement.name} onpointermove={() => index = i} onpointerdown={(e) => e.preventDefault()} onclick={() => choose(engagement.name)}>
            <span>{engagement.name}</span><span class="check">{#if engagement.name === value}<Icon name="check" size={14} />{/if}</span>
          </button>
        {/each}
      </div>
      {#if !filtered.length}<div class="no-results" role="status">No engagements found</div>{/if}
    </div>
  {/if}
</div>

<style>
  .picker { min-width: 0; }
  .trigger { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-width: 100px; max-width: min(240px, 40vw); background: var(--pd-bg); }
  .trigger[aria-expanded=true] { border-color: var(--pd-accent); }
  .value { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .trigger :global(svg) { flex: none; color: var(--pd-text-muted); }
  .menu { position: fixed; z-index: 30; display: flex; flex-direction: column; overflow: hidden; padding: 5px; background: var(--pd-elevated); border: 1px solid var(--pd-control-border); border-radius: 8px; box-shadow: 0 8px 24px var(--pd-shadow); }
  .search { display: flex; align-items: center; gap: 6px; margin: 2px 3px 5px; padding-left: 5px; color: var(--pd-text-muted); border-bottom: 1px solid var(--pd-divider); }
  .search input { flex: 1; width: 0; min-width: 0; padding: 7px 4px; border: 0; border-radius: 0; background: transparent; }
  .search input:focus { box-shadow: none; }
  .search input:focus-visible { outline: none; }
  .search:focus-within { border-bottom-color: var(--pd-accent); outline: 2px solid var(--pd-accent); outline-offset: 2px; border-radius: 4px; }
  .options { min-height: 0; overflow: auto; }
  .option { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; padding: 7px 9px; border: 0; background: transparent; text-align: left; }
  .option span:first-child { min-width: 0; overflow-wrap: anywhere; }
  .option:hover:not(:disabled), .option.active { background: var(--pd-surface); }
  .option.selected { color: var(--pd-text); }.option.active { background: var(--pd-selected); color: var(--pd-text); }
  .check { display: flex; flex: none; width: 14px; }
  .no-results { padding: 12px 9px; color: var(--pd-text-muted); font-size: 0.75rem; }
</style>
