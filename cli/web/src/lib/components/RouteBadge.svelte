<script lang="ts">
  import type { Route } from '../types';

  let { route }: { route: Route } = $props();
</script>

{#if route}
  <span class="badge route route-{route}" title="Configured route; does not confirm VPN connectivity or target reachability">{route}</span>
{:else}
  <span class="badge" title="created before route labels existed">unlabelled</span>
{/if}
