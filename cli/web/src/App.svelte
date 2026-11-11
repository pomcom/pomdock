<script lang="ts">
  import { tick } from 'svelte';
  import { app, init, loadEngagements, selectEngagement, setPref } from './lib/app.svelte';
  import { route, navigate, type View } from './lib/router.svelte';
  import Containers from './lib/components/Containers.svelte';
  import Sessions from './lib/components/Sessions.svelte';
  import Shells from './lib/components/Shells.svelte';
  import Toast from './lib/components/Toast.svelte';
  import Tracker from './lib/components/Tracker.svelte';
  import Icon from './lib/components/Icon.svelte';
  import EngagementPicker from './lib/components/EngagementPicker.svelte';
  import Modal from './lib/components/Modal.svelte';
  import { trackActivation, returnFocusTarget } from './lib/focus';

  const NAV: { view: View; label: string; icon: string; engagement: boolean }[] = [
    { view: 'sessions', label: 'Session history', icon: 'sessions', engagement: true },
    { view: 'tracker', label: 'Tracker', icon: 'tracker', engagement: true },
    { view: 'containers', label: 'Containers', icon: 'containers', engagement: false },
    { view: 'shells', label: 'Terminals', icon: 'shells', engagement: false },
  ];
  const current = $derived(NAV.find(i => i.view === route.view)!);
  let viewport = $state(window.innerWidth), mobileNav = $state(false), settings = $state(false), palette = $state(false);
  let paletteQuery = $state(''), paletteIndex = $state(0);
  let paletteInput: HTMLInputElement | undefined = $state();
  let navResizing = $state(false);
  const navCollapsed = $derived(app.prefs.navCollapsed ?? false);
  const navWidth = $derived(Math.max(176, Math.min(280, app.prefs.navWidth ?? 216)));
  let systemDark = $state(window.matchMedia('(prefers-color-scheme: dark)').matches);
  init();
  $effect(() => { document.addEventListener('click', trackActivation, true); return () => document.removeEventListener('click', trackActivation, true); });
  $effect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const change = () => systemDark = media.matches;
    media.addEventListener('change', change); return () => media.removeEventListener('change', change);
  });
  $effect(() => {
    document.documentElement.dataset.theme = app.prefs.theme === 'light' || app.prefs.theme === 'dark' ? app.prefs.theme : systemDark ? 'dark' : 'light';
    document.documentElement.dataset.density = app.prefs.density ?? 'comfortable';
  });
  $effect(() => { const name = route.params.name; if (name && name !== app.engagement && app.engagements.some(e => e.name === name)) selectEngagement(name, true); });
  $effect(() => { route.view; mobileNav = false; });
  $effect(() => {
    if (!mobileNav || viewport > 760) return;
    const previous = returnFocusTarget();
    const nav = document.querySelector('nav')!;
    const controls = () => [...nav.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled)')];
    tick().then(() => controls()[0]?.focus());
    function trap(e: KeyboardEvent) {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); mobileNav = false; }
      if (e.key === 'Tab') { const nodes = controls(), first = nodes[0], last = nodes.at(-1); if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); } else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); } }
    }
    nav.addEventListener('keydown', trap);
    return () => { nav.removeEventListener('keydown', trap); tick().then(() => previous?.focus({ preventScroll: true })); };
  });
  function href(item: typeof NAV[number]) { return `#/${item.view}${item.engagement && app.engagement ? `?name=${encodeURIComponent(app.engagement)}` : ''}`; }
  const commands = $derived([
    ...NAV.map(item => ({ label: `Go to ${item.label}`, hint: item.engagement ? 'Current engagement' : 'Workspace', run: () => navigate(item.view, item.engagement && app.engagement ? { name: app.engagement } : {}) })),
    { label: 'Search current view', hint: 'Sessions or Tracker', run: () => window.dispatchEvent(new Event('pomdock:search')) },
    { label: 'Appearance and density', hint: 'Workspace preferences', run: () => settings = true },
    { label: 'Browse recordings', hint: 'Current engagement', run: () => { navigate('sessions', app.engagement ? { name: app.engagement } : {}); tick().then(() => window.dispatchEvent(new Event('pomdock:browse'))); } },
  ].filter(c => c.label.toLowerCase().includes(paletteQuery.toLowerCase())));
  async function openPalette() { mobileNav = false; paletteQuery = ''; paletteIndex = 0; palette = true; await tick(); paletteInput?.focus(); }
  function runCommand(index: number) { const command = commands[index]; if (!command) return; palette = false; tick().then(command.run); }
  function shortcuts(e: KeyboardEvent) {
    if (e.defaultPrevented || (e.target instanceof HTMLElement && e.target.closest('.xterm, [data-terminal]'))) return;
    // F2 avoids browser address/search, print, and developer-console shortcuts.
    if (e.key === 'F2' && !settings && !document.querySelector('[role=dialog]')) { e.preventDefault(); openPalette(); }
  }
  function resizeKey(e: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault(); setPref('navWidth', e.key === 'Home' ? 176 : e.key === 'End' ? 280 : Math.max(176, Math.min(280, navWidth + (e.key === 'ArrowLeft' ? -8 : 8))));
  }
</script>
<svelte:window bind:innerWidth={viewport} onkeydown={shortcuts} />
<div class="shell" class:collapsed={navCollapsed} class:mobile-open={mobileNav} style:--nav-width={`${navWidth}px`}>
  {#if mobileNav}<button class="nav-scrim" aria-label="Close navigation" onclick={() => mobileNav = false}></button>{/if}
  <nav aria-label="Primary navigation" inert={viewport <= 760 && !mobileNav}>
    <div class="brand"><span class="mark">p</span><span class="wordmark">pom dock</span><span class="grow"></span><button class="sm ghost" aria-label="Collapse navigation" onclick={() => viewport <= 760 ? mobileNav = false : setPref('navCollapsed', true)}><Icon name="panel" /></button></div>
    <div class="section">Engagement</div>
    <div class="nav-engagement"><EngagementPicker engagements={app.engagements} value={app.engagement} onchange={selectEngagement} disabled={app.engagementBusy} /></div>
    {#each NAV as item (item.view)}
      {#if item.view === 'containers'}<div class="section workspace">Workspace</div>{/if}
      <a class="navitem" class:active={route.view === item.view} href={href(item)} title={item.label} aria-label={item.label} aria-current={route.view === item.view ? 'page' : undefined} onclick={() => mobileNav = false}><Icon name={item.icon} /><span class="navlabel">{item.label}</span></a>
    {/each}
    <div class="foot"><span>Local workspace</span><span>{app.meta?.version ?? ''}</span></div>
    {#if !navCollapsed}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
      <div class="nav-resize" role="separator" aria-label="Resize navigation" aria-orientation="vertical" aria-valuemin={176} aria-valuemax={280} aria-valuenow={navWidth} tabindex="0" onkeydown={resizeKey} onpointerdown={(e) => { (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId); navResizing = true; }} onpointermove={(e) => { if (navResizing) setPref('navWidth', Math.max(176, Math.min(280, e.clientX))); }} onpointerup={() => navResizing = false} onlostpointercapture={() => navResizing = false}></div>
    {/if}
  </nav>
  <div class="content" inert={viewport <= 760 && mobileNav}>
    <header class="context-header">
      <button class="sm ghost navtoggle" aria-label={viewport <= 760 ? 'Show navigation' : navCollapsed ? 'Expand navigation' : 'Collapse navigation'} aria-expanded={viewport <= 760 ? mobileNav : !navCollapsed} onclick={() => viewport <= 760 ? mobileNav = !mobileNav : setPref('navCollapsed', !navCollapsed)}><Icon name="panel" /></button>
      <span class="crumb">{current.label}</span>
      {#if current.engagement}<div class="context-engagement"><span class="context-label">Engagement</span>{#if navCollapsed || viewport <= 760}<EngagementPicker engagements={app.engagements} value={app.engagement} onchange={selectEngagement} disabled={app.engagementBusy} />{:else}<span class="engagement-name" title={app.engagement}>{app.engagement || 'No engagement'}</span>{/if}</div><button class="sm ghost" title="Refresh engagements" aria-label="Refresh engagements" onclick={() => loadEngagements(true).catch(e => app.error = e.message)}><Icon name="refresh" size={16} /></button>{:else}<span class="scope">All engagements</span>{/if}
      <span class="grow"></span>
      <button class="sm ghost command-trigger" aria-label="Open command palette" onclick={openPalette}><Icon name="command" /><span>Commands</span><kbd>F2</kbd></button>
      <button class="sm ghost" title="Appearance and density" aria-label="Appearance and density" onclick={() => settings = true}><Icon name="settings" /></button>
    </header>
    <main id="main-content">
      {#if app.error}<div class="empty" role="alert"><strong>Workspace unavailable</strong><p>{app.error}</p><button onclick={() => { app.error = ''; init(); }}>Retry</button></div>
      {:else if route.view === 'tracker'}<Tracker />{:else if route.view === 'sessions'}<Sessions />{:else if route.view === 'containers'}<Containers />{:else}<Shells />{/if}
    </main>
  </div>
</div>
<Toast />
{#if settings}
  <Modal title="Appearance and density" onclose={() => settings = false}>
    <fieldset><legend>Theme</legend><div class="preference-options">{#each ['system', 'light', 'dark'] as theme}<button class:chosen={(app.prefs.theme ?? 'system') === theme} aria-pressed={(app.prefs.theme ?? 'system') === theme} onclick={() => setPref('theme', theme as 'system' | 'light' | 'dark')}>{theme[0].toUpperCase() + theme.slice(1)}</button>{/each}</div></fieldset>
    <fieldset><legend>Density</legend><div class="preference-options">{#each ['comfortable', 'compact'] as density}<button class:chosen={(app.prefs.density ?? 'comfortable') === density} aria-pressed={(app.prefs.density ?? 'comfortable') === density} onclick={() => setPref('density', density as 'comfortable' | 'compact')}>{density[0].toUpperCase() + density.slice(1)}</button>{/each}</div></fieldset>
    <p class="preference-note">Density changes spacing, keeping text sizes stable. Native tmux terminals use their own font and palette.</p>
  </Modal>
{/if}
{#if palette}
  <Modal title="Commands" onclose={() => palette = false}>
    <input class="palette-search" bind:this={paletteInput} role="combobox" aria-label="Find a command" aria-expanded="true" aria-controls="palette-list" aria-activedescendant={commands.length ? `palette-${Math.min(paletteIndex, commands.length - 1)}` : undefined} bind:value={paletteQuery} oninput={() => paletteIndex = 0} placeholder="Go to a view or find an action…" onkeydown={(e) => { if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); paletteIndex = (paletteIndex + (e.key === 'ArrowDown' ? 1 : -1) + commands.length) % (commands.length || 1); } else if (e.key === 'Enter') { e.preventDefault(); runCommand(paletteIndex); } }} />
    <div id="palette-list" role="listbox" aria-label="Commands">{#each commands as command, i}<button id={`palette-${i}`} class="palette-option" class:chosen={i === paletteIndex} role="option" aria-selected={i === paletteIndex} tabindex="-1" onclick={() => runCommand(i)}><span>{command.label}</span><small>{command.hint}</small></button>{:else}<p role="status">No matching commands</p>{/each}</div>
  </Modal>
{/if}
<style>
  .shell { display: grid; grid-template-columns: var(--nav-width) minmax(0, 1fr); height: 100%; }.shell.collapsed { grid-template-columns: 56px minmax(0, 1fr); }
  nav { position: relative; display: flex; flex-direction: column; gap: 4px; background: var(--pd-sidebar); border-right: 1px solid var(--pd-divider); padding: 16px 12px; min-width: 0; }
  .brand { display: flex; align-items: center; gap: 8px; min-height: 36px; margin-bottom: 20px; font-size: 1.0625rem; font-weight: 600; white-space: nowrap; }.brand button { padding: 4px; }
  .wordmark { min-width: 0; overflow: hidden; text-overflow: ellipsis; }.mark { display: grid; place-content: center; color: var(--pd-accent); border: 1px solid var(--pd-control-border); width: 1.75rem; height: 1.75rem; border-radius: 6px; font-family: var(--pd-mono); flex: none; }
  .section { color: var(--pd-text-muted); font-size: .75rem; padding: 0 8px 4px; }.workspace { margin-top: 24px; }
  .nav-engagement { margin-bottom: 8px; }.nav-engagement :global(.trigger) { width: 100%; max-width: 100%; }
  .navitem { display: flex; align-items: center; gap: 10px; min-height: var(--pd-control-height); padding: 8px; border-radius: 8px; color: var(--pd-text-muted); text-decoration: none; white-space: normal; }.navitem:hover { background: var(--pd-hover); color: var(--pd-text); }.navitem.active { background: var(--pd-nav-active); color: var(--pd-text); box-shadow: inset 2px 0 var(--pd-accent); }.navitem.active :global(svg) { color: var(--pd-accent); }
  .foot { margin-top: auto; padding-top: 24px; display: flex; flex-wrap: wrap; gap: 8px; justify-content: space-between; font-size: .75rem; color: var(--pd-text-muted); }
  .nav-resize { position: absolute; right: -3px; top: 0; bottom: 0; width: 6px; cursor: col-resize; z-index: 4; touch-action: none; }.nav-resize:hover { background: var(--pd-control-border); }
  .collapsed .wordmark, .collapsed .brand button, .collapsed .navlabel, .collapsed .section, .collapsed .foot, .collapsed .nav-engagement { display: none; }.collapsed nav { padding-inline: 8px; }.collapsed .brand { justify-content: center; }.collapsed .navitem { justify-content: center; padding: 8px; }
  .content { display: flex; flex-direction: column; min-width: 0; min-height: 0; }.context-header { min-height: 56px; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; padding: 8px 16px; background: var(--pd-surface); border-bottom: 1px solid var(--pd-divider); }.crumb { font-weight: 600; white-space: nowrap; }.context-engagement { display: flex; gap: 8px; align-items: center; min-width: 0; padding-left: 12px; margin-left: 4px; border-left: 1px solid var(--pd-divider); }.engagement-name { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.context-label, .scope { color: var(--pd-text-muted); font-size: .75rem; }.command-trigger { display: flex; align-items: center; gap: 6px; }main { flex: 1; min-height: 0; overflow: hidden; }
  .nav-scrim { display: none; }fieldset { border: 0; padding: 0; margin: 20px 0; }legend { font-weight: 600; margin-bottom: 8px; }.preference-options { display: flex; gap: 8px; flex-wrap: wrap; }.chosen { background: var(--pd-selected); box-shadow: inset 0 0 0 1px var(--pd-accent); color: var(--pd-text); }.preference-note { color: var(--pd-text-muted); font-size: .8125rem; }
  .palette-search { width: 100%; margin: 12px 0; }.palette-option { display: flex; flex-direction: column; width: 100%; text-align: left; border: 0; background: transparent; margin: 4px 0; padding: 10px; }.palette-option.chosen { background: var(--pd-selected); }.palette-option small { color: var(--pd-text-muted); }
  @media (max-width: 1100px) { .context-label, .command-trigger span, .command-trigger kbd { display: none; }.context-engagement { padding-left: 8px; } }
  @media (max-width: 760px) { .shell, .shell.collapsed { grid-template-columns: minmax(0, 1fr); }nav { position: fixed; inset: 0 auto 0 0; width: min(280px, calc(100vw - 32px)); z-index: 16; visibility: hidden; }.mobile-open nav { visibility: visible; }.mobile-open .nav-scrim { display: block; position: fixed; inset: 0; z-index: 15; border: 0; border-radius: 0; background: rgb(0 0 0 / .4); }.collapsed .wordmark, .collapsed .brand button, .collapsed .navlabel, .collapsed .section, .collapsed .foot, .collapsed .nav-engagement { display: revert; }.collapsed .brand, .collapsed .navitem { justify-content: initial; }.nav-resize { display: none; }.context-header { padding: 8px; gap: 4px; }.context-engagement { border: 0; padding: 0; margin: 0; }.crumb { margin-right: auto; }.context-engagement :global(.trigger) { max-width: 140px; }.context-header > .grow { flex: 0; } }
</style>
