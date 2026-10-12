<script lang="ts">
  import { api } from '../api';
  import { app, selectEngagement, toast } from '../app.svelte';
  import { navigate } from '../router.svelte';
  import type { ContainerInfo, Job, Route, ShellOpened } from '../types';
  import CopyButton from './CopyButton.svelte';
  import JobLog from './JobLog.svelte';
  import Modal from './Modal.svelte';
  import RouteBadge from './RouteBadge.svelte';

  let containers: ContainerInfo[] = $state([]);
  let jobs: Job[] = $state([]);
  let error = $state('');
  let loaded = $state(false);
  let openJob = $state(''); // job whose log is expanded
  let shellInfo: (ShellOpened & { name: string }) | null = $state(null);
  let checks: Record<string, { tool: string; output: string; error?: string; busy: boolean }> = $state({});

  async function refresh() {
    try {
      [containers, jobs] = await Promise.all([api.containers(), api.jobs()]);
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

  async function openShell(c: ContainerInfo) {
    try {
      shellInfo = { ...(await api.openShell(c.name)), name: c.name };
      refresh();
    } catch (e) {
      toast((e as Error).message);
    }
  }

  async function check(c: ContainerInfo, tool: 'identity' | 'ports' | 'tor') {
    checks[c.name] = { tool, output: '', busy: true };
    try {
      const r = await api.check(c.name, tool);
      checks[c.name] = { tool, output: r.output, error: r.error, busy: false };
    } catch (e) {
      checks[c.name] = { tool, output: '', error: (e as Error).message, busy: false };
    }
  }

  function openEngagement(c: ContainerInfo, view: 'tracker' | 'sessions') {
    if (!app.engagements.some((e) => e.name === c.engagement)) {
      toast(`no atuin history for ${c.engagement} yet`);
      return;
    }
    selectEngagement(c.engagement);
    navigate(view, { name: c.engagement });
  }

  // --- create / edit-route form ------------------------------------------------

  type Form = { mode: 'create' | 'recreate'; name: string; route: Route; vpnFile: string };
  let form = $state<Form | null>(null);
  let confirmRemove = $state<ContainerInfo | null>(null);

  const knownVPNs = $derived([...new Set(containers.map((c) => c.vpnFile).filter((v): v is string => !!v))]);
  const needsVPN = $derived(form?.route === 'vpn' || form?.route === 'tor-vpn');

  function newForm() {
    form = { mode: 'create', name: '', route: 'vpn', vpnFile: knownVPNs[0] ?? '' };
  }

  function editForm(c: ContainerInfo) {
    form = { mode: 'recreate', name: c.name, route: c.route || 'direct', vpnFile: c.vpnFile ?? knownVPNs[0] ?? '' };
  }

  async function submit() {
    if (!form) return;
    const vpn = needsVPN ? form.vpnFile.trim() : '';
    if (needsVPN && !vpn) return toast('pick a VPN profile for this route');
    const whonix = form.route === 'tor' || form.route === 'tor-vpn';
    const f = form;
    form = null;
    await run(f.mode === 'create' ? api.createContainer(f.name.trim(), vpn, whonix) : api.recreateContainer(f.name, vpn, whonix));
  }

  const ROUTES: { value: Route; label: string; hint: string }[] = [
    { value: 'vpn', label: 'VPN', hint: 'gluetun kill switch' },
    { value: 'tor', label: 'Tor', hint: 'Whonix-style gateway' },
    { value: 'tor-vpn', label: 'Tor over VPN', hint: 'kali → Tor → VPN' },
    { value: 'direct', label: 'Direct', hint: 'no tunnel' },
  ];
  const fmtTime = (s: number) => new Date(s * 1000).toLocaleTimeString();
</script>

<div class="view">
  <div class="bar">
    <button class="accent" onclick={newForm}>＋ New container</button>
    <button class="sm ghost" onclick={refresh}>↻ refresh</button>
    <span class="grow"></span>
    <span class="muted">Removing or recreating keeps the loot dir, history, recordings and tracker curation.</span>
  </div>

  <div class="scroll">
    {#if shellInfo}
      <div class="notice">
        <div>
          Shell for <b>{shellInfo.name}</b> is open in tmux window <code>{shellInfo.windowId}</code> of the
          <code>{app.meta?.workspace ?? 'pomdock'}</code> workspace.
          {#if shellInfo.attached}It is selected in the terminal attached to the workspace.{:else}Attach from a terminal:{/if}
        </div>
        <div class="cmds">
          <code>{shellInfo.attach}</code><CopyButton text={shellInfo.attach} />
          <span class="muted">inside tmux:</span>
          <code>{shellInfo.switch}</code><CopyButton text={shellInfo.switch} />
        </div>
        <button class="sm ghost close" onclick={() => (shellInfo = null)}>✕</button>
      </div>
    {/if}

    {#if error}
      <div class="empty">{error}</div>
    {:else if loaded && !containers.length}
      <div class="empty">No pomdock containers yet. Create one to start an engagement.</div>
    {/if}

    <div class="cards">
      {#each containers as c (c.name)}
        {@const running = c.state === 'running'}
        <div class="card" class:running>
          <div class="top">
            <span class="dot" class:on={running} title={c.state}></span>
            <span class="name">{c.name}</span>
            <RouteBadge route={c.route} />
            {#if c.hasVpn}<span class="badge">vpn sidecar up</span>{/if}
            {#if c.hasTor}<span class="badge">tor sidecar up</span>{/if}
            {#if c.legacy}<span class="badge" title="created before contextual prompt metadata; recreate to upgrade">legacy</span>{/if}
            <span class="grow"></span>
            {#if c.job}<span class="badge busy">working…</span>{/if}
          </div>
          <div class="sub">
            <span>{c.image}</span>
            {#if c.vpnFile}<span title="VPN profile">{c.vpnFile}</span>{/if}
            {#if c.shell}<span>shell open ({c.shell})</span>{/if}
          </div>
          <div class="actions">
            {#if running}
              <button class="sm accent" disabled={!!c.job} onclick={() => openShell(c)}>{c.shell ? 'Show shell' : 'Open shell'}</button>
              <button class="sm" disabled={!!c.job} onclick={() => run(api.stopContainer(c.name))}>Stop</button>
            {:else}
              <button class="sm accent" disabled={!!c.job} onclick={() => run(api.startContainer(c.name))}>Start</button>
            {/if}
            <button class="sm" disabled={!!c.job} onclick={() => editForm(c)}>Change route…</button>
            <button class="sm danger" disabled={!!c.job} onclick={() => (confirmRemove = c)}>Remove…</button>
            <span class="sep"></span>
            <button class="sm ghost" onclick={() => openEngagement(c, 'tracker')}>Tracker</button>
            <button class="sm ghost" onclick={() => openEngagement(c, 'sessions')}>Sessions</button>
            {#if running}
              <span class="sep"></span>
              <button class="sm ghost" onclick={() => check(c, 'identity')}>Egress</button>
              <button class="sm ghost" onclick={() => check(c, 'tor')}>Tor check</button>
              <button class="sm ghost" onclick={() => check(c, 'ports')}>Ports</button>
            {/if}
          </div>
          {#if checks[c.name]}
            {@const ck = checks[c.name]}
            <div class="check">
              <div class="checkhead">
                <span>{ck.tool}</span>
                <span class="grow"></span>
                <button class="sm ghost" onclick={() => delete checks[c.name]}>✕</button>
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
  <Modal title={form.mode === 'create' ? 'New pentest container' : `Change route of ${form.name}`} onclose={() => (form = null)}>
    {#if form.mode === 'create'}
      <label class="field">
        <span>Engagement name</span>
        <input bind:value={form.name} placeholder="acme" pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]*" />
        <small>Container name and loot dir <code>~/pentest/{form.name || 'NAME'}</code></small>
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
          <input type="radio" bind:group={form.route} value={r.value} />
          <RouteBadge route={r.value} /> <span>{r.label}</span> <small>{r.hint}</small>
        </label>
      {/each}
    </div>
    {#if needsVPN}
      <label class="field">
        <span>VPN profile (WireGuard / OpenVPN file on this host)</span>
        <input bind:value={form.vpnFile} list="vpnfiles" placeholder="~/vpn/mullvad.conf" />
        <datalist id="vpnfiles">{#each knownVPNs as v (v)}<option value={v}></option>{/each}</datalist>
      </label>
    {/if}
    <div class="submit">
      <button class="accent" disabled={form.mode === 'create' && !form.name.trim()} onclick={submit}>
        {form.mode === 'create' ? 'Create' : 'Recreate'}
      </button>
    </div>
  </Modal>
{/if}

{#if confirmRemove}
  {@const c = confirmRemove}
  <Modal title={`Remove ${c.name}?`} onclose={() => (confirmRemove = null)}>
    <p class="warn">
      Removes the container and its VPN/Tor sidecars{c.shell ? ' (its open shell window ends too)' : ''}. The loot dir,
      atuin history, recordings and tracker curation stay on the host; <code>pomdock docker exec --name {c.name}</code>
      or “New container” with the same name picks them up again.
    </p>
    <div class="submit">
      <button class="danger" onclick={() => { confirmRemove = null; run(api.removeContainer(c.name)); }}>Remove</button>
    </div>
  </Modal>
{/if}

<style>
  .view { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .scroll { flex: 1; overflow: auto; padding: 16px 18px 40px; }
  .muted { color: var(--overlay); font-size: 11px; }
  .notice {
    position: relative; background: color-mix(in srgb, var(--accent) 12%, var(--mantle)); border: 1px solid var(--accent);
    border-radius: 11px; padding: 12px 40px 12px 14px; margin-bottom: 14px; display: grid; gap: 8px;
  }
  .notice .close { position: absolute; top: 8px; right: 8px; }
  .cmds { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
  .cards { display: grid; gap: 12px; }
  .card { background: var(--mantle); border: 1px solid var(--surface1); border-radius: 12px; padding: 12px 14px; }
  .top { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--faint); flex: none; }
  .dot.on { background: var(--green); box-shadow: 0 0 0 3px color-mix(in srgb, var(--green) 25%, transparent); }
  .name { font-weight: 650; font-size: 14px; font-family: var(--mono); }
  .busy { background: color-mix(in srgb, var(--yellow) 25%, transparent); color: var(--yellow); }
  .sub { display: flex; gap: 14px; flex-wrap: wrap; color: var(--overlay); font-size: 11px; margin: 4px 0 10px 17px; font-family: var(--mono); }
  .actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin-left: 17px; }
  .sep { width: 1px; height: 18px; background: var(--surface1); margin: 0 4px; }
  .check { margin: 10px 0 0 17px; }
  .checkhead { display: flex; align-items: center; color: var(--overlay); font-size: 10px; text-transform: uppercase; letter-spacing: .5px; font-weight: 600; margin-bottom: 4px; }
  .err { color: var(--red); font-size: 12px; margin-top: 6px; font-family: var(--mono); white-space: pre-wrap; }
  h3 { font-size: 11px; color: var(--overlay); text-transform: uppercase; letter-spacing: .6px; margin: 26px 0 8px; }
  .jobs { display: grid; gap: 6px; }
  .job { background: var(--mantle); border: 1px solid var(--surface0); border-radius: 10px; padding: 6px 8px; }
  .jobhead { display: flex; gap: 10px; align-items: center; width: 100%; border: none; background: none; text-align: left; padding: 4px; }
  .job :global(pre.log) { margin-top: 6px; }
  .status-running { background: color-mix(in srgb, var(--yellow) 25%, transparent); color: var(--yellow); }
  .status-done { background: color-mix(in srgb, var(--green) 22%, transparent); color: var(--green); }
  .status-failed { background: color-mix(in srgb, var(--red) 22%, transparent); color: var(--red); }

  .field { display: grid; gap: 5px; margin: 12px 0; color: var(--subtext); }
  .field input { width: 100%; font-family: var(--mono); }
  .field small { color: var(--overlay); }
  .warn { color: var(--subtext); font-size: 12px; margin: 0 0 12px; }
  .routes { display: grid; gap: 6px; margin: 12px 0; }
  .routeopt {
    display: flex; gap: 8px; align-items: center; border: 1px solid var(--surface1); border-radius: 9px;
    padding: 8px 10px; cursor: pointer; color: var(--text);
  }
  .routeopt.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); }
  .routeopt small { color: var(--overlay); }
  .submit { display: flex; justify-content: flex-end; margin-top: 8px; }
</style>
