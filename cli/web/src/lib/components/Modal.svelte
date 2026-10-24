<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, onclose, children, busy = false }: { title: string; onclose: () => void; children: Snippet; busy?: boolean } = $props();

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && !busy) onclose();
  }

  function focusDialog(node: HTMLElement) {
    const previous = document.activeElement as HTMLElement | null;
    const controls = () => [...node.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]')];
    queueMicrotask(() => (node.querySelector<HTMLElement>('[autofocus]') ?? controls()[0] ?? node).focus());
    function trap(e: KeyboardEvent) {
      if (e.key !== 'Tab') return;
      const items = controls();
      const first = items[0], last = items[items.length - 1];
      if (!first) { e.preventDefault(); node.focus(); }
      else if (e.shiftKey && (document.activeElement === first || document.activeElement === node)) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
    node.addEventListener('keydown', trap);
    return { destroy() { node.removeEventListener('keydown', trap); previous?.focus(); } };
  }
</script>

<svelte:window {onkeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => !busy && e.target === e.currentTarget && onclose()}>
  <div class="box" role="dialog" aria-modal="true" aria-label={title} aria-busy={busy} tabindex="-1" use:focusDialog>
    <h2>{title}</h2>
    {@render children()}
    <div class="foot"><button class="sm" disabled={busy} onclick={onclose}>Close</button></div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed; inset: 0; background: rgba(0, 0, 0, .5); z-index: 20;
    display: flex; align-items: center; justify-content: center;
  }
  .box {
    background: var(--mantle); border: 1px solid var(--surface1); border-radius: 6px; padding: 18px;
    width: min(560px, 92vw); max-height: 80vh; overflow: auto; box-shadow: 0 20px 60px var(--shadow);
  }
  h2 { margin: 0 0 6px; font-size: 15px; }
  .foot { margin-top: 14px; text-align: right; }
</style>
