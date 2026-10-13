<script lang="ts">
  // Renders text with every case-insensitive match of q wrapped in <mark>.
  // Svelte escapes all parts, so command output can never inject markup.
  let { text = '', q = '' }: { text?: string; q?: string } = $props();

  const parts = $derived.by(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return [{ t: text, hit: false }];
    const out: { t: string; hit: boolean }[] = [];
    const hay = text.toLowerCase();
    let i = 0;
    for (let j = hay.indexOf(needle); j >= 0; j = hay.indexOf(needle, i)) {
      if (j > i) out.push({ t: text.slice(i, j), hit: false });
      out.push({ t: text.slice(j, j + needle.length), hit: true });
      i = j + needle.length;
    }
    if (i < text.length) out.push({ t: text.slice(i), hit: false });
    return out;
  });
</script>

{#each parts as p}{#if p.hit}<mark class="hl">{p.t}</mark>{:else}{p.t}{/if}{/each}
