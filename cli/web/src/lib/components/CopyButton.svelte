<script lang="ts">
  import { copyText, toast } from '../app.svelte';
  import Icon from './Icon.svelte';

  let { text, title = 'Copy', label = '', primary = false }: { text: string; title?: string; label?: string; primary?: boolean } = $props();
  let copied = $state(false);

  async function copy(ev: MouseEvent) {
    ev.stopPropagation();
    copied = false;
    try {
      await copyText(text);
      copied = true;
      setTimeout(() => (copied = false), 1100);
    } catch (e) { toast((e as Error).message); }
  }
</script>

<button class="sm copy" class:accent={primary} class:ghost={!primary} class:primary {title} aria-label={copied ? 'Copied' : title} onclick={copy}>
  {#if copied}✓{:else}<Icon name="copy" size={14} />{/if}{#if label}<span>{label}</span>{/if}
</button>

<style>
  .copy.primary { min-height: var(--pd-control-height); padding: 6px 10px; border-color: var(--pd-accent); }
  .copy { flex: none; display: inline-flex; gap: 5px; align-items: center; justify-content: center; min-width: 28px; min-height: 28px; padding: 4px; border-color: transparent; }
</style>
