<script lang="ts">
  import { api } from '../api';
  import { app, toast } from '../app.svelte';
  import type { ShellInfo } from '../types';
  import CopyButton from './CopyButton.svelte';

  // The pomdock tmux workspace's shell windows. tmux is the source of truth:
  // this view only lists what tmux reports and can close pomdock's own windows.

  let shells: ShellInfo[] = $state([]);
  let attach = $state('');
  let switchCmd = $state('');
  let error = $state('');
  let loaded = $state(false);

  async function refresh() {
    try {
      const r = await api.shells();
      shells = r.shells;
      attach = r.attach;
      switchCmd = r.switch;
      error = '';
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loaded = true;
    }
  }

  $effect(() => {
    refresh();
    const timer = setInterval(() => document.visibilityState === 'visible' && refresh(), 4000);
    return () => clearInterval(timer);
  });

  async function close(s: ShellInfo) {
    if (!confirm(`Close tmux window ${s.name}? Anything running in that shell ends; its recording is kept.`)) return;
    try {
      await api.closeShell(s.id);
      toast(`closed ${s.name}`);
      refresh();
    } catch (e) {
      toast((e as Error).message);
    }
  }
</script>

<div class="view">
  <div class="bar">
    <span>Workspace <code>{app.meta?.workspace ?? 'pomdock'}</code></span>
    {#if attach}
      <span class="muted">attach:</span><code>{attach}</code><CopyButton text={attach} />
      <span class="muted">inside tmux:</span><code>{switchCmd}</code><CopyButton text={switchCmd} />
    {/if}
    <span class="grow"></span>
    <button class="sm ghost" onclick={refresh}>↻ refresh</button>
  </div>
  <div class="scroll">
    {#if error}
      <div class="empty">{error}</div>
    {:else if loaded && !shells.length}
      <div class="empty">No shell windows. Open one from <a href="#/containers">Containers</a>; every container shell is recorded.</div>
    {:else}
      <table>
        <thead><tr><th>#</th><th>Window</th><th>Kind</th><th>Target</th><th></th></tr></thead>
        <tbody>
          {#each shells as s (s.id)}
            <tr>
              <td class="mono">{s.index}</td>
              <td class="mono">{s.name}{#if s.active}<span class="badge active">active</span>{/if}</td>
              <td>{s.kind}</td>
              <td class="mono">{s.target}</td>
              <td class="act"><button class="sm danger" onclick={() => close(s)}>Close</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

<style>
  .view { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .scroll { flex: 1; overflow: auto; padding: 8px 18px 40px; }
  .muted { color: var(--overlay); font-size: 11px; }
  a { color: var(--accent); }
  table { border-collapse: collapse; width: 100%; }
  th { text-align: left; font-size: 11px; color: var(--overlay); text-transform: uppercase; letter-spacing: .3px; padding: 9px 10px; border-bottom: 1px solid var(--surface1); }
  td { padding: 9px 10px; border-bottom: 1px solid var(--surface0); }
  .act { text-align: right; }
  .active { margin-left: 8px; background: color-mix(in srgb, var(--accent) 25%, transparent); color: var(--accent); }
</style>
