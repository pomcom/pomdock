<script lang="ts">
  import CopyButton from './CopyButton.svelte';
  let { command, target, label = 'Copy connect command' }: { command?: string; target: string; label?: string } = $props();
</script>

<div class="connect-command" role="group" aria-label={`Connection command for ${target}`}>
  {#if command}
    <code>{command}</code>
    <CopyButton text={command} title={`${label} for ${target}`} {label} primary />
  {:else}
    <span class="unavailable">Connect command unavailable. Refresh to retrieve it.</span>
  {/if}
</div>

<style>
  .connect-command { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; padding: 8px 10px; background: var(--pd-surface); border: 1px solid var(--pd-divider); border-radius: 6px; }
  code { flex: 1 1 220px; min-width: 0; white-space: pre-wrap; overflow-wrap: anywhere; user-select: text; font-size: .8125rem; line-height: 1.5; }
  .unavailable { color: var(--pd-text-muted); font-size: .8125rem; }
</style>
