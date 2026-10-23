<script lang="ts">
  import { fuzzyIndices } from '../tracker';

  // Renders text with matches of q wrapped in <mark>. In substring mode (the
  // default) every case-insensitive occurrence of q is marked; in fuzzy mode
  // the individual subsequence characters fzf would match are marked instead.
  // When curStart is the char offset of a specific occurrence, that one match
  // gets the `cur` class and curId, so a find widget can scroll to / emphasise
  // the "current" result. Svelte escapes all parts, so output can never inject
  // markup.
  let {
    text = '', q = '', fuzzy = false, curStart = -1, curId = '',
  }: { text?: string; q?: string; fuzzy?: boolean; curStart?: number; curId?: string } = $props();

  type Part = { t: string; hit: boolean; cur?: boolean };

  const parts = $derived.by<Part[]>(() => {
    const needle = q.trim();
    if (!needle) return [{ t: text, hit: false }];

    if (fuzzy) {
      const idx = fuzzyIndices(text, needle);
      if (!idx || !idx.length) return [{ t: text, hit: false }];
      const mark = new Set(idx);
      const out: Part[] = [];
      let buf = '';
      let bufHit = false;
      for (let i = 0; i < text.length; i++) {
        const hit = mark.has(i);
        if (i > 0 && hit !== bufHit) {
          out.push({ t: buf, hit: bufHit });
          buf = '';
        }
        buf += text[i];
        bufHit = hit;
      }
      if (buf) out.push({ t: buf, hit: bufHit });
      return out;
    }

    const low = needle.toLowerCase();
    const out: Part[] = [];
    const hay = text.toLowerCase();
    let i = 0;
    for (let j = hay.indexOf(low); j >= 0; j = hay.indexOf(low, i)) {
      if (j > i) out.push({ t: text.slice(i, j), hit: false });
      out.push({ t: text.slice(j, j + low.length), hit: true, cur: j === curStart });
      i = j + low.length;
    }
    if (i < text.length) out.push({ t: text.slice(i), hit: false });
    return out;
  });
</script>

{#each parts as p}{#if p.hit}<mark class="hl" class:cur={p.cur} id={p.cur ? curId : undefined}>{p.t}</mark>{:else}{p.t}{/if}{/each}
