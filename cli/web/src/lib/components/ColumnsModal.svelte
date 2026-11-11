<script lang="ts">
  import Modal from './Modal.svelte';
  import { COLUMNS } from '../tracker';

  let { hidden, onchange, onclose }: {
    hidden: string[];
    onchange: (hidden: string[]) => void;
    onclose: () => void;
  } = $props();

  const optional = COLUMNS.filter((c) => c.optional);

  function toggle(key: string, on: boolean) {
    onchange(on ? hidden.filter((k) => k !== key) : [...hidden, key]);
  }
</script>

<Modal title="Columns" {onclose}>
  <p>Pick the columns for this sheet. The choice drives both the grid and the exported TSV.</p>
  <div class="presets">
    <button class="sm accent" onclick={() => onchange(['mitre'])}>Tester sheet</button>
    <button class="sm accent" onclick={() => onchange(['tester'])}>MITRE sheet</button>
    <button class="sm" onclick={() => onchange([])}>Both</button>
  </div>
  {#each optional as c (c.key)}
    <label class="row">
      <input type="checkbox" checked={!hidden.includes(c.key)} onchange={(e) => toggle(c.key, e.currentTarget.checked)} />
      {c.header}
    </label>
  {/each}
</Modal>

<style>
  p { color: var(--pd-text-muted); margin: 0 0 12px; font-size: 0.75rem; }
  .presets { display: flex; gap: 8px; margin-bottom: 12px; }
  .row {
    display: flex; gap: 8px; align-items: center; padding: 7px 0; color: var(--pd-text);
    border-bottom: 1px solid var(--pd-surface); cursor: pointer;
  }
</style>
