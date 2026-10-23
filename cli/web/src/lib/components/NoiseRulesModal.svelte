<script lang="ts">
  import { untrack } from 'svelte';
  import Modal from './Modal.svelte';

  let { rules, prefill = '', onchange, onclose }: {
    rules: string[];
    prefill?: string;
    onchange: (rules: string[]) => void;
    onclose: () => void;
  } = $props();

  let draft = $state(untrack(() => prefill)); // initial value only

  function add() {
    const v = draft.trim();
    if (v && !rules.includes(v)) onchange([...rules, v]);
    draft = '';
  }
</script>

<Modal title="Noise rules" {onclose}>
  <p>
    Rows whose command matches a rule are treated as noise and hidden unless “show noise” is on.
    Plain text is a substring match; wrap in <code>/…/</code> for a regex. Saved for all engagements.
  </p>
  {#each rules as rule, i (rule)}
    <div class="rule">
      <span class="pat">{rule}</span>
      <button class="sm ghost" onclick={() => onchange(rules.filter((_, j) => j !== i))}>remove</button>
    </div>
  {:else}
    <p class="none">No custom rules yet.</p>
  {/each}
  <div class="rule">
    <!-- svelte-ignore a11y_autofocus -->
    <input class="pat" aria-label="New noise rule" bind:value={draft} autofocus placeholder="e.g. ifconfig.me  or  /^curl/"
      onkeydown={(e) => e.key === 'Enter' && add()} />
    <button class="sm accent" onclick={add}>add</button>
  </div>
</Modal>

<style>
  p { color: var(--subtext); margin: 0 0 12px; font-size: 12px; }
  .none { margin: 0; }
  .rule { display: flex; gap: 8px; align-items: center; padding: 6px 0; border-bottom: 1px solid var(--surface0); }
  .pat { flex: 1; font-family: var(--mono); font-size: 12px; }
</style>
