<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, onclose, children }: { title: string; onclose: () => void; children: Snippet } = $props();

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose();
  }
</script>

<svelte:window {onkeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <div class="box" role="dialog" aria-modal="true" aria-label={title}>
    <h2>{title}</h2>
    {@render children()}
    <div class="foot"><button class="sm" onclick={onclose}>done</button></div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed; inset: 0; background: rgba(0, 0, 0, .5); z-index: 20;
    display: flex; align-items: center; justify-content: center;
  }
  .box {
    background: var(--mantle); border: 1px solid var(--surface1); border-radius: 14px; padding: 18px;
    width: min(560px, 92vw); max-height: 80vh; overflow: auto; box-shadow: 0 20px 60px var(--shadow);
  }
  h2 { margin: 0 0 6px; font-size: 15px; }
  .foot { margin-top: 14px; text-align: right; }
</style>
