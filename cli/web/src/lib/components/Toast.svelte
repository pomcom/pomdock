<script lang="ts">
  import { toastState } from '../app.svelte';

  function undo() {
    toastState.undo?.();
    toastState.visible = false;
  }

  function dismiss() {
    toastState.visible = false;
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions, a11y_no_static_element_interactions -->
<div class="toast" class:show={toastState.visible} role="status" onclick={dismiss} title="click to dismiss">
  {toastState.message}
  {#if toastState.undo}<button class="sm" onclick={(e) => (e.stopPropagation(), undo())}>undo</button>{/if}
</div>

<style>
  .toast {
    position: fixed; bottom: 22px; left: 50%; transform: translateX(-50%) translateY(8px);
    background: var(--accent); color: var(--base); padding: 10px 20px; border-radius: 11px; font-weight: 600;
    opacity: 0; transition: .2s; pointer-events: none; z-index: 30; box-shadow: 0 10px 30px var(--shadow);
    display: flex; gap: 12px; align-items: center; max-width: 90vw;
  }
  .toast.show { opacity: 1; transform: translateX(-50%) translateY(0); pointer-events: auto; }
  button { background: transparent; color: var(--base); border-color: var(--base); font-weight: 700; }
</style>
