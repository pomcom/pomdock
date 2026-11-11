<script lang="ts">
  import { tick } from 'svelte';
  import { api } from '../api';
  import { app, toast } from '../app.svelte';
  import type { ShellInfo } from '../types';
  import CopyButton from './CopyButton.svelte';
  import ConnectCommand from './ConnectCommand.svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import VmConnections from './VmConnections.svelte';

  // Observed tmux windows, not direct shells or browser terminal connections.
  let shells: ShellInfo[] = $state([]);
  let warnings: string[] = $state([]);
  let error = $state('');
  let loaded = $state(false), refreshedAt = $state(0), busy = $state('');
  let confirmClose: ShellInfo | null = $state(null);
  let vmRefreshToken = $state(0);
  const countKnown = (value: number | undefined): value is number => value !== undefined && Number.isInteger(value) && value >= 0;
  async function refresh() {
    vmRefreshToken = Date.now();
    try {
      const result = await api.shells();
      shells = result.shells; warnings = result.warnings ?? [];
      error = ''; refreshedAt = Date.now();
    } catch (e) { error = (e as Error).message; }
    finally { loaded = true; }
  }
  $effect(() => { refresh(); const timer = setInterval(() => document.visibilityState === 'visible' && refresh(), 4000); return () => clearInterval(timer); });
  async function close(shell: ShellInfo) {
    busy = shell.id;
    try { await api.closeShell(shell.id); confirmClose = null; toast(`Closed ${shell.name} · recording kept`); await refresh(); await tick(); document.querySelector<HTMLButtonElement>('[aria-label="Refresh terminals"]')?.focus({ preventScroll: true }); }
    catch (e) { toast((e as Error).message); }
    finally { busy = ''; }
  }
</script>
<div class="view">
  <div class="bar">
    <span>Terminal connections</span>
    <span class="muted">{shells.length} tmux windows{#if refreshedAt} · updated {new Date(refreshedAt).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' })}{/if}</span>
    <span class="grow"></span><button class="sm ghost" aria-label="Refresh terminals" onclick={refresh}><Icon name="refresh" /> Refresh</button>
  </div>
  <div class="scroll">
    {#if error}<div class="error" role="alert">Could not refresh terminals: {error}{#if refreshedAt}<p>Showing the last retrieved tmux state.</p>{/if}<button class="sm" onclick={refresh}>Retry</button></div>{/if}
    {#if warnings.length}<div class="discovery-warning" role="status"><strong>Some tmux windows could not be inspected.</strong>{#each warnings as warning}<p>{warning}</p>{/each}</div>{/if}
    {#if !loaded}<div class="empty" role="status">Loading tmux windows…</div>
    {:else if !shells.length && !error}<div class="empty"><strong>{warnings.length ? 'No tmux windows were returned' : 'No tmux windows yet'}</strong><p>Copy a connect command from <a href="#/containers">Containers</a> and run it in your terminal. Direct shells without tmux are not listed here.</p></div>
    {:else if shells.length}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-scroll" role="region" aria-label="Native terminal windows" tabindex="0">
        <table><caption class="sr-only">Observed container and existing host tmux windows. Selected window and attached session clients are tmux state, not browser connection state.</caption>
          <thead><tr><th scope="col">Window</th><th scope="col">Connection</th><th scope="col">Context</th><th scope="col">Actions</th></tr></thead>
          <tbody>{#each shells as shell (shell.id)}
            {@const inContainer = shell.location === 'container'}
            <tr><td><div class="window-name mono">{shell.name}</div><div class="meta"><code>{shell.session}</code></div><div class="meta">Window {shell.index} · <code>{shell.windowId ?? shell.id}</code>{#if shell.active}<span class="badge">Selected window</span>{/if}</div>{#if countKnown(shell.clients)}<div class="meta" title={`Clients attached to tmux session ${shell.session}, not browser connections`}>{shell.clients} attached tmux {shell.clients === 1 ? 'client' : 'clients'} · session</div>{/if}{#if countKnown(shell.panes)}<div class="meta">{shell.panes} {shell.panes === 1 ? 'pane' : 'panes'}</div>{/if}</td>
              <td class="connection"><ConnectCommand command={shell.attach} target={shell.name} />
                {#if !inContainer && shell.switch}<div class="secondary-command"><span>Inside host tmux</span><code>{shell.switch}</code><CopyButton text={shell.switch} title={`Copy switch command for ${shell.target}`} /></div>{/if}
                {#if shell.direct}<div class="secondary-command"><span>Shell without tmux</span><code>{shell.direct}</code><CopyButton text={shell.direct} title={`Copy direct shell command for ${shell.target}`} /></div>{/if}
              </td><td><div class="context"><code>{shell.target}</code><span class="badge">{inContainer ? 'Container tmux' : 'Host tmux · existing shell'}</span>{#if shell.kind === 'vm'}<span class="meta">VM terminal</span>{/if}{#if shell.kind === 'docker' && app.engagements.some(e => e.name === shell.target)}<a href={`#/sessions?name=${encodeURIComponent(shell.target)}`}>Recordings</a>{/if}</div></td>
              <td><button class="sm ghost danger" disabled={!!busy} aria-label={`Close terminal ${shell.name}`} onclick={() => confirmClose = shell}>Close…</button></td></tr>
          {/each}</tbody>
        </table>
      </div>
      <p class="native-note">Only tmux windows are listed. Direct shells are not tracked here. Input, selection, ANSI colors and shortcuts stay in your terminal.</p>
    {/if}
    <VmConnections refreshToken={vmRefreshToken} />
  </div>
</div>
{#if confirmClose}
  <Modal title={`Close ${confirmClose.name}?`} busy={!!busy} onclose={() => confirmClose = null}><p>Ends tmux window <code>{confirmClose.windowId ?? confirmClose.id}</code>{confirmClose.location === 'container' ? ' inside container ' : ' on the host for '}<code>{confirmClose.target}</code> and all running processes in its panes. Saved recordings are kept.</p><button class="danger" disabled={!!busy} onclick={() => confirmClose && close(confirmClose)}>Close terminal</button></Modal>
{/if}
<style>
  .view { height: 100%; display: flex; flex-direction: column; min-height: 0; }.scroll { flex: 1; min-height: 0; overflow: auto; padding: 16px 24px 32px; }.muted, .meta, .native-note { font-size: .8125rem; color: var(--pd-text-muted); }.secondary-command { display: grid; grid-template-columns: auto minmax(160px, 1fr) auto; align-items: center; gap: 4px 8px; margin-top: 8px; font-size: .8125rem; }.secondary-command span { color: var(--pd-text-muted); }.secondary-command code { white-space: pre-wrap; overflow-wrap: anywhere; }.table-scroll { overflow: auto; background: var(--pd-surface); border: 1px solid var(--pd-divider); border-radius: 8px; }
  table { width: 100%; border-collapse: separate; border-spacing: 0; min-width: 760px; background: var(--pd-surface); }th { position: sticky; top: 0; padding: 12px; text-align: left; color: var(--pd-text-muted); background: var(--pd-sidebar); font-size: .8125rem; font-weight: 600; border-bottom: 1px solid var(--pd-divider); }td { padding: 12px; border-bottom: 1px solid var(--pd-divider); height: var(--pd-row-height); vertical-align: top; }tr:hover { background: var(--pd-hover); }.window-name { font-size: .875rem; overflow-wrap: anywhere; }.meta { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; font-variant-numeric: tabular-nums; }.context { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; }.connection { min-width: 320px; }td a { display: inline-block; min-height: 24px; padding-block: 2px; }
  .error { color: var(--pd-danger); margin: 16px 0; }.discovery-warning { color: var(--pd-warning); margin-bottom: 16px; font-size: .8125rem; }.discovery-warning p { margin: 4px 0 0; }
  :global([data-density=compact]) td, :global([data-density=compact]) th { padding-block: 8px; }
  @media (max-width: 600px) { .scroll { padding: 12px 8px; }.bar { padding-inline: 8px; } }
</style>
