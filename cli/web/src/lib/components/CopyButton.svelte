<script lang="ts">
  import { copyText } from '../app.svelte';

  let { text, title = 'copy', label = 'copy' }: { text: string; title?: string; label?: string } = $props();
  let copied = $state(false);

  async function copy(ev: MouseEvent) {
    ev.stopPropagation();
    await copyText(text);
    copied = true;
    setTimeout(() => (copied = false), 1100);
  }
</script>

<button class="sm ghost copy" {title} onclick={copy}>{copied ? '✓ copied' : label}</button>

<style>
  .copy { flex: none; }
</style>
