<script lang="ts">
  import { api } from '../api';
  import { app, selectEngagement, toast } from '../app.svelte';
  import { navigate } from '../router.svelte';
  import type { ContainerInfo, Job, Route } from '../types';
  import CopyButton from './CopyButton.svelte';
  import ConnectCommand from './ConnectCommand.svelte';
  import JobLog from './JobLog.svelte';
  import Modal from './Modal.svelte';
  import RouteBadge from './RouteBadge.svelte';

  let containers: ContainerInfo[] = $state([]);
  let jobs: Job[] = $state([]);
  let error = $state('');
  let loaded = $state(false);
  let refreshedAt = $state(0);
  let warnings: string[] = $state([]);
  let openJob = $state(''); // job whose log is expanded
  let checks: Record<string, { tool: string; output: string; error?: string; busy: boolean; checkedAt?: number }> = $state({});

  async function refresh() {
    try {
      const [state, currentJobs] = await Promise.all([api.containerState(), api.jobs()]);
      containers = state.containers; warnings = state.warnings ?? []; jobs = currentJobs;
      error = ''; refreshedAt = Date.now();
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

  async function run(p: Promise<Job>) {
    try {
      const job = await p;
      openJob = job.id;
      await refresh();
    } catch (e) {
      toast((e as Error).message);
    }
  }

  function jobDone(job: Job) {
    refresh();
    toast(`${job.kind} ${job.target}: ${job.status === 'done' ? 'done' : 'failed'}`);
  }

  async function check(c: ContainerInfo, tool: 'identity' | 'ports' | 'tor') {
    checks[c.name] = { tool, output: '', busy: true };
    try {
      const r = await api.check(c.name, tool);
      checks[c.name] = { tool, output: r.output, error: r.error, busy: false, checkedAt: Date.now() };
    } catch (e) {
      checks[c.name] = { tool, output: '', error: (e as Error).message, busy: false };
    }
  }

  function openEngagement(c: ContainerInfo, view: 'tracker' | 'sessions') {
    if (!app.engagements.some((e) => e.name === c.engagement)) {
      toast(`No recordings or tracker data for ${c.engagement} yet.`);
      return;
    }
    selectEngagement(c.engagement);
    navigate(view, { name: c.engagement });
  }

  // --- create / edit-route form ------------------------------------------------

  type Form = { mode: 'create' | 'recreate'; name: string; route: Route; vpnFile: string };
  let form = $state<Form | null>(null);
  let formBusy = $state(false), formError = $state('');
  let confirmRemove = $state<ContainerInfo | null>(null);

  const knownVPNs = $derived([...new Set(containers.map((c) => c.vpnFile).filter((v): v is string => !!v))]);
  const needsVPN = $derived(form?.route === 'vpn' || form?.route === 'tor-vpn');

  function newForm() {
    formError = '';
    form = { mode: 'create', name: '', route: 'vpn', vpnFile: knownVPNs[0] ?? '' };
  }

  function editForm(c: ContainerInfo) {
    formError = '';
    form = { mode: 'recreate', name: c.name, route: c.route || 'direct', vpnFile: c.vpnFile ?? knownVPNs[0] ?? '' };
  }

  async function submit() {
    if (!form || formBusy) return;
    const vpn = needsVPN ? form.vpnFile.trim() : '';
    if (needsVPN && !vpn) { formError = 'Choose a VPN profile for this route.'; return; }
    const current = form;
    const whonix = current.route === 'tor' || current.route === 'tor-vpn';
    formBusy = true; formError = '';
    try {
      const job = await (current.mode === 'create' ? api.createContainer(current.name.trim(), vpn, whonix) : api.recreateContainer(current.name, vpn, whonix));
      openJob = job.id; form = null; await refresh();
    } catch (e) { formError = (e as Error).message; }
    finally { formBusy = false; }
  }

  const ROUTES: { value: Route; label: string; hint: string }[] = [
    { value: 'vpn', label: 'VPN', hint: 'gluetun kill switch' },
    { value: 'tor', label: 'Tor', hint: 'Whonix-style gateway' },
    { value: 'tor-vpn', label: 'Tor over VPN', hint: 'kali → Tor → VPN' },
    { value: 'direct', label: 'Direct', hint: 'no tunnel' },
  ];
  const fmtTime = (s: number) => new Date(s * 1000).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' });
</script>

<div class="view">
  <div class="bar">
    <button class="accent" onclick={newForm}>New container</button>
    <button class="sm ghost" onclick={refresh}>Refresh</button>
    <span class="grow"></span>
    <span class="muted" role="status">Docker runtime{#if refreshedAt} · updated {new Date(refreshedAt).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' })}{/if}</span>
  </div>

  <div class="scroll">
    {#if error}
      <div class="error" role="alert">Refresh failed: {error}{#if refreshedAt}<span>Showing the last retrieved state.</span>{/if}<button class="sm" onclick={refresh}>Retry</button></div>
    {:else if !loaded}<div class="empty" role="status">Loading containers…</div>{:else if !containers.length}
      <div class="empty">No pomdock containers yet. Create one to start an engagement.</div>
    {/if}

    {#if warnings.length}<div class="discovery-warning" role="status"><strong>Some runtime details are unavailable.</strong>{#each warnings as warning}<p>{warning}</p>{/each}</div>{/if}

    <div class="cards">
      {#each containers as c (c.name)}
        {@const running = c.state === 'running'}
        <div class="card" class:running>
          <div class="top">
            <span class="dot" class:on={running} title={c.state}></span>
            <span class="name">{c.name}</span>{#if c.engagement !== c.name}<span class="engagement">Engagement: {c.engagement}</span>{/if}
            <span class="muted">{c.state}</span>
            <RouteBadge route={c.route} />
            {#if c.hasVpn}<span class="badge">VPN sidecar running</span>{/if}
            {#if c.hasTor}<span class="badge">Tor sidecar running</span>{/if}
            {#if c.legacy}<span class="badge" title="created before contextual prompt metadata; recreate to upgrade">legacy</span>{/if}
            <span class="grow"></span>
            {#if c.job}<span class="badge busy">working…</span>{/if}
          </div>
          <div class="sub">
            <span>{c.image}</span>

            {#if c.session}<span>tmux · <code>{c.session}</code></span>{/if}
          </div>
          <ConnectCommand command={c.connect} target={c.name} />
          <div class="actions">
            <div class="action-group" role="group" aria-label={`Container controls for ${c.name}`}>
              {#if running}<button class="sm" disabled={!!c.job} onclick={() => run(api.stopContainer(c.name))}>Stop</button>
              {:else}<button class="sm" disabled={!!c.job} onclick={() => run(api.startContainer(c.name))}>Start</button>{/if}
              <button class="sm" disabled={!!c.job} onclick={() => editForm(c)}>Change route…</button>
            </div>
            <div class="action-group" role="group" aria-label={`Engagement views for ${c.name}`}>
              <button class="sm ghost" onclick={() => openEngagement(c, 'tracker')}>Tracker</button>
              <button class="sm ghost" onclick={() => openEngagement(c, 'sessions')}>Session history</button>
            </div>
            {#if running}<div class="action-group" role="group" aria-label={`Diagnostics for ${c.name}`}>
              <button class="sm ghost" onclick={() => check(c, 'identity')}>Egress</button>
              <button class="sm ghost" onclick={() => check(c, 'tor')}>Tor check</button>
              <button class="sm ghost" onclick={() => check(c, 'ports')}>Ports</button>
            </div>{/if}
            <span class="grow"></span>
            <button class="sm ghost danger" disabled={!!c.job} onclick={() => (confirmRemove = c)}>Remove…</button>
          </div>
          <div class="direct-shell"><span>Shell without tmux</span><code>{c.direct}</code><CopyButton text={c.direct} title={`Copy direct shell command for ${c.name}`} /></div>
          <details class="container-details"><summary>Connection details</summary><dl><dt>Container</dt><dd><code>{c.name}</code><CopyButton text={c.name} title="Copy container name" /></dd><dt>Image</dt><dd>{c.image}</dd><dt>Engagement</dt><dd>{c.engagement}</dd><dt>Configured route</dt><dd>{c.route || 'Not labelled'}</dd>{#if c.vpnFile}<dt>VPN profile</dt><dd>{c.vpnFile}</dd>{/if}<dt>Detach from container tmux</dt><dd><span><kbd>Ctrl-b</kbd> <kbd>d</kbd>; from host tmux, <kbd>Ctrl-b</kbd> <kbd>Ctrl-b</kbd> <kbd>d</kbd>.</span></dd><dt>Connectivity</dt><dd>Not inferred from runtime or sidecar presence. Use an explicit check above.</dd></dl></details>
          {#if checks[c.name]}
            {@const ck = checks[c.name]}
            <div class="check">
              <div class="checkhead">
                <span>{ck.tool}{#if ck.checkedAt} · checked {new Date(ck.checkedAt).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' })}{/if}</span>
                <span class="grow"></span>
                <button class="sm ghost" title="Close check output" aria-label="Close check output" onclick={() => delete checks[c.name]}>✕</button>
              </div>
              <pre class="log">{ck.busy ? 'running…' : ck.output || '(no output)'}</pre>
              {#if ck.error}<div class="err">{ck.error}</div>{/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>

    {#if jobs.length}
      <h3>Operations</h3>
      <div class="jobs">
        {#each jobs as j (j.id)}
          <div class="job">
            <button class="jobhead" onclick={() => (openJob = openJob === j.id ? '' : j.id)}>
              <span class="badge status-{j.status}">{j.status}</span>
              <span class="mono">{j.kind} {j.target}</span>
              <span class="grow"></span>
              <span class="muted">{fmtTime(j.started)}</span>
            </button>
            {#if openJob === j.id}<JobLog id={j.id} ondone={jobDone} />{/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

{#if form}
  <Modal title={form.mode === 'create' ? 'New container' : `Change route of ${form.name}`} busy={formBusy} onclose={() => (form = null)}>
    <form onsubmit={(e) => { e.preventDefault(); submit(); }}>
    {#if form.mode === 'create'}
      <label class="field">
        <span>Engagement name</span>
        <input required maxlength="63" disabled={formBusy} bind:value={form.name} placeholder="acme" pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]*" />
        <small>Workspace folder <code>{app.meta?.workspaceRoot || '~/pomdock/workspaces'}/{form.name || 'NAME'}</code></small>
      </label>
    {:else}
      <p class="warn">
        The route is fixed when a container is created, so this removes <b>{form.name}</b> and its sidecars and
        creates it again. The loot dir, atuin history, recordings and tracker curation are kept; anything installed
        inside the container is lost.
      </p>
    {/if}
    <div class="routes">
      {#each ROUTES as r (r.value)}
        <label class="routeopt" class:on={form.route === r.value}>
          <input type="radio" disabled={formBusy} bind:group={form.route} value={r.value} />
          <RouteBadge route={r.value} /> <span>{r.label}</span> <small>{r.hint}</small>
        </label>
      {/each}
    </div>
    {#if needsVPN}
      <label class="field">
        <span>VPN profile (WireGuard / OpenVPN file on this host)</span>
        <input required disabled={formBusy} bind:value={form.vpnFile} list="vpnfiles" placeholder="~/vpn/mullvad.conf" />
        <datalist id="vpnfiles">{#each knownVPNs as v (v)}<option value={v}></option>{/each}</datalist>
      </label>
    {/if}
    {#if formError}<p class="err" role="alert">{formError}</p>{/if}
    <div class="submit">
      <button type="submit" class="accent" disabled={formBusy || (form.mode === 'create' && !form.name.trim())}>
        {formBusy ? 'Starting operation…' : form.mode === 'create' ? 'Create' : 'Recreate'}
      </button>
    </div>
    </form>
  </Modal>
{/if}

{#if confirmRemove}
  {@const c = confirmRemove}
  <Modal title={`Remove ${c.name}?`} onclose={() => (confirmRemove = null)}>
    <p class="warn">
      Removes the container, its tmux windows and running processes, and its VPN/Tor sidecars. The workspace,
      atuin history, recordings and tracker curation stay on the host; <code>pomdock docker exec --name {c.name}</code>
      or “New container” with the same name picks them up again.
    </p>
    <div class="submit">
      <!-- {@const} is a lazy derived: read the name before clearing confirmRemove. -->
      <button class="danger" onclick={() => { const name = c.name; confirmRemove = null; run(api.removeContainer(name)); }}>Remove</button>
    </div>
  </Modal>
{/if}

<style>
 .direct-shell { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 12px; font-size: .8125rem; }.direct-shell span { color: var(--pd-text-muted); }.direct-shell code { white-space: pre-wrap; overflow-wrap: anywhere; }
  .view { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .scroll { flex: 1; min-height: 0; overflow: auto; padding: 0 24px 32px; }
  .muted { color: var(--pd-text-muted); font-size: .8125rem; }
  .cards { display: grid; }
  .card { min-width: 0; background: transparent; border-bottom: 1px solid var(--pd-divider); padding: var(--pd-section-gap) 0; }
  .top { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--pd-text-muted); flex: none; }
  .dot.on { background: var(--pd-success); }
  .name { overflow-wrap: anywhere; font-weight: 650; font-size: 0.875rem; font-family: var(--pd-mono); }
  .busy { background: color-mix(in srgb, var(--pd-warning) 25%, transparent); color: var(--pd-warning); }
  .sub { overflow-wrap: anywhere; display: flex; gap: 14px; flex-wrap: wrap; color: var(--pd-text-muted); font-size: .8125rem; margin: 4px 0 10px 17px; font-family: var(--pd-mono); }
  .actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin-top: 12px; }
  .action-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .actions { gap: 8px 16px; }
  .check { margin: 10px 0 0 17px; }
  .checkhead { display: flex; align-items: center; color: var(--pd-text-muted); font-size: .8125rem;   font-weight: 600; margin-bottom: 4px; }
  .err { color: var(--pd-danger); font-size: .8125rem; margin-top: 6px; font-family: var(--pd-mono); white-space: pre-wrap; }
  h3 { font-size: .8125rem; color: var(--pd-text-muted);   margin: 26px 0 8px; }
  .jobs { display: grid; gap: 6px; }
  .job { background: var(--pd-surface); border-bottom: 1px solid var(--pd-divider); border-radius: 4px; padding: 6px 8px; }
  .jobhead { display: flex; gap: 10px; align-items: center; width: 100%; border: none; background: none; text-align: left; padding: 4px; }
  .job :global(pre.log) { margin-top: 6px; }
  .status-running { background: color-mix(in srgb, var(--pd-warning) 25%, transparent); color: var(--pd-warning); }
  .status-done { background: color-mix(in srgb, var(--pd-success) 22%, transparent); color: var(--pd-success); }
  .status-failed { background: color-mix(in srgb, var(--pd-danger) 22%, transparent); color: var(--pd-danger); }

  .field { display: grid; gap: 5px; margin: 12px 0; color: var(--pd-text-muted); }
  .field input { width: 100%; font-family: var(--pd-mono); }
  .field small { color: var(--pd-text-muted); overflow-wrap: anywhere; }
  .warn { color: var(--pd-text-muted); font-size: .8125rem; margin: 0 0 12px; }
  .routes { display: grid; gap: 6px; margin: 12px 0; }
  .routeopt {
    display: flex; gap: 8px; align-items: center; border: 1px solid var(--pd-divider); border-radius: 5px;
    padding: 8px 10px; cursor: pointer; color: var(--pd-text);
  }
  .routeopt.on { border-color: var(--pd-accent); background: color-mix(in srgb, var(--pd-accent) 10%, transparent); }
  .routeopt small { color: var(--pd-text-muted); }
  .submit { display: flex; justify-content: flex-end; margin-top: 8px; }
  .engagement { color: var(--pd-text-muted); font-size: .8125rem; }.error { padding: 16px; color: var(--pd-danger); display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }.container-details { margin: 8px 0 0 17px; font-size: .8125rem; color: var(--pd-text-muted); }dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; padding: 8px 0; }dd { margin: 0; display: flex; align-items: center; gap: 8px; overflow-wrap: anywhere; }.routeopt { flex-wrap: wrap; }.discovery-warning { color: var(--pd-warning); margin: 12px 0; font-size: .8125rem; }.discovery-warning p { margin: 4px 0 0; }
  @media (max-width: 600px) { .scroll { padding-inline: 12px; }.actions, .sub, .container-details { margin-left: 0; }.muted { overflow-wrap: anywhere; } }
</style>
