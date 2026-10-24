<script lang="ts">
  import { copyText } from '../app.svelte';
  import Icon from './Icon.svelte';

  let { text, title = 'Copy', label = '' }: { text: string; title?: string; label?: string } = $props();
  let copied = $state(false);

  async function copy(ev: MouseEvent) {
    ev.stopPropagation();
    await copyText(text);
    copied = true;
    setTimeout(() => (copied = false), 1100);
  }
</script>

<button class="sm ghost copy" {title} aria-label={copied ? 'Copied' : title} onclick={copy}>
  {#if copied}✓{:else}<Icon name="copy" size={14} />{/if}{#if label}<span>{label}</span>{/if}
</button>

<style>
  .copy { flex: none; display: inline-flex; gap: 5px; align-items: center; justify-content: center; min-width: 28px; min-height: 28px; padding: 4px; border-color: transparent; }
</style>
