<script lang="ts">
  import type { Route } from '../types';

  let { route }: { route: Route } = $props();
</script>

{#if route}
  <span class="badge route route-{route}" title="network route">{route}</span>
{:else}
  <span class="badge" title="created before route labels existed">unlabelled</span>
{/if}
