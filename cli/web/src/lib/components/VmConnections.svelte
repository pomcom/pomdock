<script lang="ts">
  import { onDestroy } from 'svelte';
  import { api } from '../api';
  import type { VMConnection } from '../types';
  import ConnectCommand from './ConnectCommand.svelte';
  import CopyButton from './CopyButton.svelte';

  let { refreshToken = 0 }: { refreshToken?: number } = $props();
  let vms: VMConnection[] = $state([]);
  let warnings: string[] = $state([]);
  let error = $state('');
  let loaded = $state(false), refreshing = $state(false);
  let inFlight: Promise<void> | null = null;
  let controller: AbortController | null = null;
  let destroyed = false;

  function refresh(): Promise<void> {
    if (inFlight || destroyed) return inFlight ?? Promise.resolve();
    const current = new AbortController();
    controller = current; refreshing = true;
    const task = (async () => {
      try {
        const result = await api.vmConnections(current.signal);
        if (destroyed || current.signal.aborted || controller !== current) return;
        vms = result.vms; warnings = result.warnings ?? []; error = '';
      } catch (e) {
        if (!destroyed && !current.signal.aborted && controller === current) error = (e as Error).message;
      } finally {
        if (!destroyed && controller === current) { loaded = true; refreshing = false; }
      }
    })();
    inFlight = task;
    void task.finally(() => { if (inFlight === task) inFlight = null; });
    return task;
  }
  $effect(() => { refreshToken; void refresh(); });
  onDestroy(() => { destroyed = true; controller?.abort(); });
</script>

<section class="vm-connections" aria-label="VM connections">
  <h2>VM connections</h2>
  {#if error}<div class="vm-error" role="alert"><span>VM connections unavailable: {error}{#if vms.length}{' Showing last retrieved VM details.'}{/if}</span><button class="sm" disabled={refreshing} onclick={refresh}>Retry</button></div>{/if}
  {#if warnings.length}<div class="vm-warning" role="status">{#each warnings as warning}<p>{warning}</p>{/each}</div>{/if}
  {#if !loaded}<div class="vm-empty" role="status">Loading VM connections…</div>
  {:else if !vms.length && !error}<div class="vm-empty">{warnings.length ? 'No VM connections were returned.' : 'No profiled VMs.'}</div>
  {:else if vms.length}
    <ul class="vm-list">
      {#each vms as vm (vm.name)}
        <li class="vm-row">
          <div class="vm-identity"><strong class="mono">{vm.name}</strong><div class="vm-meta"><span>{vm.profileLabel}</span><span class="badge">{vm.state}</span>{#if vm.rdpClient}<code>{vm.rdpClient}</code>{/if}</div><div class="vm-meta">{#if vm.rdpUser}<span>User <code>{vm.rdpUser}</code></span>{/if}{#if vm.ip}<span>IP <code>{vm.ip}</code></span>{/if}</div></div>
          <div class="vm-command">
            {#if vm.rdpCommand}<ConnectCommand command={vm.rdpCommand} target={vm.name} label="Copy RDP command" />
            {:else}<p class="vm-reason">{vm.rdpReason || 'RDP command unavailable.'}</p>{/if}
            {#if vm.rdpCommand && vm.rdpReason}<p class="vm-reason">{vm.rdpReason}</p>{/if}
            {#if vm.connect}<details class="vm-wrapper"><summary>Pomdock command</summary><div><code>{vm.connect}</code><CopyButton text={vm.connect} title={`Copy Pomdock RDP command for ${vm.name}`} label="Copy Pomdock RDP command" /></div></details>{/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .vm-connections { margin-top: var(--pd-section-gap); padding-top: 16px; border-top: 1px solid var(--pd-divider); }
  h2 { margin: 0 0 12px; font-size: .875rem; font-weight: 600; }
  .vm-list { list-style: none; padding: 0; margin: 0; }
  .vm-row { display: grid; grid-template-columns: minmax(160px, 240px) minmax(0, 1fr); gap: 12px 20px; padding: 12px 0; border-bottom: 1px solid var(--pd-divider); }
  .vm-identity, .vm-command { min-width: 0; }
  .vm-identity strong { font-size: .875rem; font-weight: 600; overflow-wrap: anywhere; }
  .vm-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; margin-top: 6px; font-size: .8125rem; color: var(--pd-text-muted); }
  .vm-reason, .vm-empty { color: var(--pd-text-muted); font-size: .8125rem; margin: 4px 0; overflow-wrap: anywhere; }
  .vm-wrapper { margin-top: 8px; color: var(--pd-text-muted); font-size: .8125rem; }
  .vm-wrapper summary { min-height: 24px; cursor: pointer; }
  .vm-wrapper div { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 12px; padding-top: 4px; }
  .vm-wrapper code { white-space: pre-wrap; overflow-wrap: anywhere; user-select: text; }
  .vm-error { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--pd-danger); font-size: .8125rem; margin-bottom: 8px; overflow-wrap: anywhere; }
  .vm-warning { color: var(--pd-warning); font-size: .8125rem; margin-bottom: 8px; }
  .vm-warning p { margin: 4px 0; overflow-wrap: anywhere; }
  :global([data-density=compact]) .vm-row { padding-block: 8px; }
  @media (max-width: 760px) { .vm-row { grid-template-columns: minmax(0, 1fr); gap: 8px; } }
</style>
