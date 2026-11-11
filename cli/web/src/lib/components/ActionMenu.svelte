<script lang="ts">
  import { tick, type Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  let { label, children }: { label: string; children: Snippet } = $props();
  let open = $state(false);
  let root: HTMLDivElement;
  let trigger: HTMLButtonElement;
  let menu: HTMLDivElement | undefined = $state();
  let left = $state(0), top = $state(0), maxHeight = $state(400);
  const id = $props.id();
  const items = () => Array.from(menu?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []);
  function position() {
    if (!open || !trigger) return;
    const r = trigger.getBoundingClientRect();
    left = Math.max(8, Math.min(r.right - 240, innerWidth - 248));
    const height = Math.min(menu?.scrollHeight || 240, innerHeight - 16);
    top = r.bottom + height + 12 <= innerHeight ? r.bottom + 4 : Math.max(8, r.top - height - 4);
    maxHeight = innerHeight - top - 8;
  }
  function close(restore = false) { open = false; if (restore) trigger?.focus({ preventScroll: true }); }
  async function show() { open = true; position(); await tick(); position(); items()[0]?.focus(); }
  function keys(e: KeyboardEvent) {
    const controls = items(), at = controls.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true); }
    else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) {
      e.preventDefault(); controls[e.key === 'Home' ? 0 : e.key === 'End' ? controls.length - 1 : (at + (e.key === 'ArrowDown' ? 1 : -1) + controls.length) % controls.length]?.focus();
    } else if (e.key === 'Tab') close();
  }
</script>
<svelte:window onresize={position} onpointerdown={(e) => { if (open && !root.contains(e.target as Node)) close(); }} />
<div class="action-menu" bind:this={root} onfocusout={(e) => { if (open && !root.contains(e.relatedTarget as Node)) close(); }}>
  <button class="sm ghost trigger" bind:this={trigger} aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} aria-controls={id} onclick={() => open ? close(true) : show()} onkeydown={(e) => { if (e.key === 'ArrowDown') { e.preventDefault(); show(); } }}><Icon name="more" size={20} /></button>
  {#if open}
    <!-- svelte-ignore a11y_interactive_supports_focus -->
    <div class="menu" bind:this={menu} id={id} role="menu" aria-label={label} style:left={`${left}px`} style:top={`${top}px`} style:max-height={`${maxHeight}px`} onkeydown={keys} onclick={(e) => { if ((e.target as HTMLElement).closest('button')) close(true); }}>
      {@render children()}
    </div>
  {/if}
</div>
<style>
  .action-menu { flex: none; }.trigger { min-width: 32px; }
  .menu { position: fixed; z-index: 35; width: min(240px, calc(100vw - 16px)); padding: 6px; overflow: auto; background: var(--pd-elevated); border: 1px solid var(--pd-control-border); border-radius: 8px; box-shadow: 0 8px 24px var(--pd-shadow); }
  .menu :global(button) { display: flex; align-items: center; width: 100%; text-align: left; border: 0; background: none; padding: 8px 10px; }
  .menu :global(button:hover), .menu :global(button:focus-visible) { background: var(--pd-hover); }
</style>
