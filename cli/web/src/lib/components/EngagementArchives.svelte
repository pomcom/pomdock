<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api';
  import { app, loadEngagements, flushTrackerEdits, flushPreferences, setPref } from '../app.svelte';
  import type { EngagementArchive } from '../types';
  import Modal from './Modal.svelte';

  let { name, onchanged, disabled = false }: { name: string; onchanged: () => Promise<void>; disabled?: boolean } = $props();
  let archives: EngagementArchive[] = $state([]);
  let expanded = $state(false);
  let action = $state<'archive' | 'restore' | ''>('');
  let target = $state('');
  let pass = $state('');
  let confirmPass = $state('');
  let busy = $state(false);
  let error = $state('');
  let listError = $state('');
  const cold = $derived(app.engagements.find(e => e.name === name)?.archived ?? false);

  async function refresh(engagement: string) {
    try { const result = await api.engagementArchives(engagement); if (engagement === name) { archives = result; listError = ''; } }
    catch (e) { if (engagement === name) listError = (e as Error).message; }
  }
  $effect(() => { const engagement = name; untrack(() => { archives = []; listError = ''; action = ''; pass = confirmPass = ''; if (engagement) refresh(engagement); }); });

  function open(kind: 'archive' | 'restore', file = '') { action = kind; target = file; pass = confirmPass = ''; error = ''; }

  async function submit() {
    if (app.engagementBusy || !pass || (action === 'archive' && pass !== confirmPass)) return;
    busy = true; app.engagementBusy = true; error = '';
    try {
      await flushPreferences();
      await flushTrackerEdits();
      if (action === 'archive') await api.archiveEngagement(name, pass);
      else {
        const result = await api.restoreEngagement(name, target, pass);
        if (result.preferences) { app.prefs = { ...app.prefs, ...result.preferences }; setPref('engagement', name); }
      }
      action = ''; pass = confirmPass = ''; expanded = true;
      await loadEngagements(true);
      await refresh(name);
      await onchanged();
    } catch (e) { error = (e as Error).message; }
    finally { busy = false; app.engagementBusy = false; }
  }
</script>

<div class="archives">
  <div class="actions">
    <button class="sm ghost" aria-expanded={expanded} onclick={() => { expanded = !expanded; if (expanded) refresh(name); }}>Archives ({archives.length})</button>
    <button class="sm" disabled={!name || cold || disabled || busy || app.engagementBusy} title="Encrypt the entire engagement, regardless of recording selection" onclick={() => open('archive')}>Archive engagement</button>
  </div>
  {#if expanded || cold}
    {#if listError}<div class="message" role="alert">{listError}</div>{/if}
    {#each archives as archive (archive.file)}
      <div class="archive">
        <span class="filename" title={archive.file}>{archive.file}</span>
        <span class="meta">{new Date(archive.created * 1000).toLocaleString('en-GB', { timeZone: 'Europe/Berlin' })} · {(archive.size / 1024).toFixed(1)} KB</span>
        <button class="sm" disabled={busy || disabled || app.engagementBusy} onclick={() => open('restore', archive.file)}>Restore</button>
      </div>
    {:else}<div class="message">No encrypted engagement archives</div>{/each}
  {/if}
</div>

{#if action}
  <Modal title={action === 'archive' ? 'Archive & encrypt engagement' : 'Restore engagement'} {busy} onclose={() => { if (!busy) { action = ''; pass = confirmPass = ''; } }}>
    <form onsubmit={(e) => { e.preventDefault(); submit(); }}>
      <p>{action === 'archive' ? 'Packages original history, recordings, session edits and Tracker into loot/archives. Live history is removed after verification. Stop engagement containers first.' : 'Restores the original history and GUI edits. Existing live history is never overwritten.'}</p>
      {#if target}<p class="filename">{target}</p>{/if}
      <label>Passphrase<input type="password" autocomplete={action === 'archive' ? 'new-password' : 'current-password'} bind:value={pass} disabled={busy} /></label>
      {#if action === 'archive'}<label>Confirm passphrase<input type="password" autocomplete="new-password" bind:value={confirmPass} disabled={busy} /></label>{/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="accent" type="submit" disabled={busy || !pass || (action === 'archive' && pass !== confirmPass)}>{busy ? 'Working…' : action === 'archive' ? 'Archive & encrypt' : 'Restore'}</button>
    </form>
  </Modal>
{/if}

<style>
  .archives { flex: none; max-height: 40%; overflow: auto; padding: 8px; border-top: 1px solid var(--pd-divider); }
  .actions { display: flex; align-items: center; justify-content: space-between; gap: 4px; flex-wrap: wrap; }
  .archive { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 4px 8px; padding: 10px 4px; border-bottom: 1px solid var(--pd-divider); }
  .filename { overflow-wrap: anywhere; font-family: var(--pd-mono); font-size: 0.75rem; }
  .archive .filename { grid-column: 1 / -1; }
  .meta, .message { color: var(--pd-text-muted); font-size: 0.75rem; }.message { padding: 10px 4px; }
  label { display: grid; gap: 5px; margin: 12px 0; }
  .error { color: var(--pd-danger); }
</style>
