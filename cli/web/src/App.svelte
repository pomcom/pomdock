<script lang="ts">
  import { app, init, loadEngagements, selectEngagement, setPref } from './lib/app.svelte';
  import { route, type View } from './lib/router.svelte';
  import Containers from './lib/components/Containers.svelte';
  import Sessions from './lib/components/Sessions.svelte';
  import Shells from './lib/components/Shells.svelte';
  import Toast from './lib/components/Toast.svelte';
  import Tracker from './lib/components/Tracker.svelte';
  import Icon from './lib/components/Icon.svelte';

  type NavItem = { view: View; label: string; icon: string; engagement: boolean };
  const NAV: { section: string; items: NavItem[] }[] = [
    {
      section: 'Engagement',
      items: [
        { view: 'tracker', label: 'Tracker', icon: 'tracker', engagement: true },
        { view: 'sessions', label: 'Session history', icon: 'sessions', engagement: true },
      ],
    },
    {
      section: 'Control',
      items: [
        { view: 'containers', label: 'Containers', icon: 'containers', engagement: false },
        { view: 'shells', label: 'Terminals', icon: 'shells', engagement: false },
      ],
    },
  ];

  const current = $derived(NAV.flatMap((s) => s.items).find((i) => i.view === route.view)!);

  let viewport = $state(window.innerWidth);
  const navCollapsed = $derived(app.prefs.navCollapsed ?? viewport < 1000);

  init();

  $effect(() => {
    document.documentElement.dataset.theme = app.prefs.theme ?? 'dark';
  });

  // Follow ?name= when the user edits the URL or uses back/forward.
  $effect(() => {
    const name = route.params.name;
    if (name && name !== app.engagement && app.engagements.some((e) => e.name === name)) selectEngagement(name, true);
  });

  function href(item: NavItem) {
    return `#/${item.view}${item.engagement && app.engagement ? `?name=${encodeURIComponent(app.engagement)}` : ''}`;
  }
</script>

<svelte:window bind:innerWidth={viewport} />
<div class="shell" class:compact={navCollapsed}>
  <nav aria-label="Primary navigation">
    <div class="brand"><span class="mark">p</span><span class="wordmark">pom dock</span></div>
    <button class="sm ghost navtoggle" title={navCollapsed ? 'Expand navigation' : 'Collapse navigation'} aria-label={navCollapsed ? 'Expand navigation' : 'Collapse navigation'} aria-expanded={!navCollapsed} onclick={() => setPref('navCollapsed', !navCollapsed)}><Icon name="panel" /></button>
    {#each NAV as section (section.section)}
      <div class="section">{section.section}</div>
      {#each section.items as item (item.view)}
        <a class="navitem" class:active={route.view === item.view} href={href(item)} title={item.label} aria-label={item.label} aria-current={route.view === item.view ? 'page' : undefined}>
          <span class="ico"><Icon name={item.icon} /></span><span class="navlabel">{item.label}</span>
        </a>
      {/each}
    {/each}
    <div class="foot">
      <span class="loopback" title="Local workspace">Local workspace</span>
      {#if app.meta}<span>{app.meta.version}</span>{/if}
    </div>
  </nav>

  <div class="content">
    <header>
      <span class="crumb">{current.label}</span>
      {#if current.engagement}
        <label class="eng">
          <span>Engagement</span>
          <select value={app.engagement} onchange={(e) => selectEngagement(e.currentTarget.value)}>
            {#each app.engagements as e (e.name)}
              <option value={e.name}>{e.name}</option>
            {/each}
          </select>
        </label>
        <button class="sm ghost" title="Refresh engagements" aria-label="Refresh engagements" onclick={() => loadEngagements(true)}><Icon name="refresh" size={14} /></button>
      {/if}
      <span class="grow"></span>
      <button class="sm ghost theme" title="Toggle light / dark theme" aria-label="Toggle light / dark theme"
        onclick={() => setPref('theme', app.prefs.theme === 'light' ? 'dark' : 'light')}>
        <Icon name="sun" />
      </button>
    </header>
    <main>
      {#if app.error}
        <div class="empty">{app.error}</div>
      {:else if route.view === 'tracker'}
        <Tracker />
      {:else if route.view === 'sessions'}
        <Sessions />
      {:else if route.view === 'containers'}
        <Containers />
      {:else if route.view === 'shells'}
        <Shells />
      {/if}
    </main>
  </div>
</div>
<Toast />

<style>
  .shell { display: grid; grid-template-columns: 190px minmax(0, 1fr); height: 100%; }
  .shell.compact { grid-template-columns: 56px minmax(0, 1fr); }
  nav { position: relative; background: var(--crust); border-right: 1px solid var(--surface1); display: flex; flex-direction: column; padding: 16px 10px 10px; gap: 3px; min-width: 0; }
  .brand { display: flex; align-items: center; gap: 9px; height: 30px; padding: 0 7px; font-size: 15px; font-weight: 650; white-space: nowrap; }
  .mark { display: grid; place-items: center; width: 22px; height: 22px; border: 1px solid var(--accent); border-radius: 5px; color: var(--accent); font-family: var(--mono); }
  .navtoggle { position: absolute; right: 10px; top: 17px; display: flex; }
  .section { color: var(--overlay); font-size: 10px; text-transform: uppercase; letter-spacing: .8px; padding: 18px 10px 5px; }
  .navitem { display: flex; gap: 10px; align-items: center; min-height: 34px; padding: 7px 9px; border-radius: 5px; color: var(--subtext); text-decoration: none; }
  .navitem:hover { background: var(--surface0); color: var(--text); }
  .navitem.active { background: var(--surface0); color: var(--text); box-shadow: inset 2px 0 var(--accent); }
  .ico { display: flex; flex: none; color: var(--overlay); }
  .navitem.active .ico { color: var(--accent); }
  .foot { margin-top: auto; display: flex; flex-wrap: wrap; gap: 6px; justify-content: space-between; padding: 12px 8px 0; font-size: 10px; color: var(--overlay); }
  .compact .wordmark, .compact .navlabel, .compact .foot { display: none; }
  .compact .brand { padding: 0 6px; }
  .compact .section { font-size: 0; padding: 12px 0 0; }
  .compact .navtoggle { position: static; align-self: center; margin-top: 8px; }
  .content { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  header { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; min-height: 52px; padding: 8px 18px; background: var(--mantle); border-bottom: 1px solid var(--surface1); }
  .crumb { font-weight: 600; font-size: 13px; }
  .eng { display: flex; gap: 8px; align-items: center; border-left: 1px solid var(--surface2); padding-left: 14px; min-width: 0; }
  .eng select { max-width: min(240px, 32vw); min-width: 80px; }
  .theme { display: flex; }
  main { flex: 1; min-height: 0; overflow: hidden; }
  @media (max-width: 600px) { header { padding: 8px 10px; } .eng { border: 0; padding: 0; } .eng span { display: none; } }
</style>
