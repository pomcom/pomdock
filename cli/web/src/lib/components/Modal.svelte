<script lang="ts">
  import type { Snippet } from 'svelte';
  import { returnFocusTarget } from '../focus';

  let { title, onclose, children, busy = false }: { title: string; onclose: () => void; children: Snippet; busy?: boolean } = $props();
  let dialogNode: HTMLDivElement | undefined = $state();

  // Disabling a focused form control can move focus to the document body.
  // Keep it inside the dialog while working, then return to an enabled field.
  $effect(() => {
    const working = busy, node = dialogNode;
    if (!node) return;
    if (!node.contains(document.activeElement) || (!working && document.activeElement === node)) {
      if (working) node.focus({ preventScroll: true });
      else (node.querySelector<HTMLElement>('input:not(:disabled), textarea:not(:disabled), button:not(:disabled)') ?? node).focus({ preventScroll: true });
    }
  });

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') { e.stopPropagation(); if (!busy) { e.preventDefault(); onclose(); } }
  }

  function focusDialog(node: HTMLElement) {
    const previous = returnFocusTarget();
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
    node.addEventListener('keydown', onkeydown);
    return { destroy() { node.removeEventListener('keydown', trap); node.removeEventListener('keydown', onkeydown); previous?.focus({ preventScroll: true }); } };
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => !busy && e.target === e.currentTarget && onclose()}>
  <div class="box" bind:this={dialogNode} role="dialog" aria-modal="true" aria-label={title} aria-busy={busy} tabindex="-1" use:focusDialog>
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
    background: var(--pd-elevated); border: 1px solid var(--pd-divider); border-radius: 12px; padding: 24px;
    width: min(560px, 92vw); max-height: 80vh; overflow: auto; box-shadow: 0 20px 60px var(--pd-shadow);
  }
  h2 { margin: 0 0 6px; font-size: 1.125rem; }
  .foot { margin-top: 14px; text-align: right; }
</style>
