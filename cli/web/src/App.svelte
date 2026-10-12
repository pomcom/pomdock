<script lang="ts">
  import { app, init, loadEngagements, selectEngagement, setPref } from './lib/app.svelte';
  import { route, type View } from './lib/router.svelte';
  import Containers from './lib/components/Containers.svelte';
  import Sessions from './lib/components/Sessions.svelte';
  import Shells from './lib/components/Shells.svelte';
  import Toast from './lib/components/Toast.svelte';
  import Tracker from './lib/components/Tracker.svelte';

  type NavItem = { view: View; label: string; icon: string; engagement: boolean };
  const NAV: { section: string; items: NavItem[] }[] = [
    {
      section: 'Engagement',
      items: [
        { view: 'tracker', label: 'Tracker', icon: '▤', engagement: true },
        { view: 'sessions', label: 'Sessions', icon: '❯', engagement: true },
      ],
    },
    {
      section: 'Control',
      items: [
        { view: 'containers', label: 'Containers', icon: '◫', engagement: false },
        { view: 'shells', label: 'Shells', icon: '⌘', engagement: false },
      ],
    },
  ];

  const current = $derived(NAV.flatMap((s) => s.items).find((i) => i.view === route.view)!);

  init();

  $effect(() => {
    document.documentElement.dataset.theme = app.prefs.theme ?? 'dark';
  });

  // Follow ?name= when the user edits the URL or uses back/forward.
  $effect(() => {
    const name = route.params.name;
    if (name && name !== app.engagement && app.engagements.some((e) => e.name === name)) selectEngagement(name);
  });

  function href(item: NavItem) {
    return `#/${item.view}${item.engagement && app.engagement ? `?name=${encodeURIComponent(app.engagement)}` : ''}`;
  }
</script>

<div class="shell">
  <nav>
    <div class="brand"><span class="mark"></span>pom<span>dock</span></div>
    {#each NAV as section (section.section)}
      <div class="section">{section.section}</div>
      {#each section.items as item (item.view)}
        <a class="navitem" class:active={route.view === item.view} href={href(item)}>
          <span class="ico">{item.icon}</span>{item.label}
        </a>
      {/each}
    {/each}
    <div class="foot">
      <span class="loopback">● 127.0.0.1</span>
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
              <option value={e.name}>{e.name} · {e.rows}</option>
            {/each}
          </select>
        </label>
        <button class="sm ghost" title="rescan engagements" onclick={() => loadEngagements(true)}>↻</button>
      {/if}
      <span class="grow"></span>
      <button class="sm ghost theme" title="toggle light / dark"
        onclick={() => setPref('theme', app.prefs.theme === 'light' ? 'dark' : 'light')}>
        {app.prefs.theme === 'light' ? '☀' : '☾'}
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
  .shell { display: grid; grid-template-columns: 190px 1fr; height: 100%; }
  nav { background: var(--crust); border-right: 1px solid var(--surface1); display: flex; flex-direction: column; padding: 14px 10px; gap: 2px; }
  .brand { display: flex; align-items: center; gap: 8px; font-size: 16px; font-weight: 700; padding: 2px 8px 14px; letter-spacing: -.2px; }
  .brand span { color: var(--accent); }
  .mark { width: 9px; height: 9px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 25%, transparent); }
  .section { color: var(--faint); font-size: 10px; text-transform: uppercase; letter-spacing: .7px; font-weight: 600; padding: 12px 10px 4px; }
  .navitem {
    display: flex; gap: 9px; align-items: center; padding: 7px 10px; border-radius: 8px;
    color: var(--subtext); text-decoration: none; border: 1px solid transparent;
  }
  .navitem:hover { background: var(--surface0); color: var(--text); }
  .navitem.active { background: var(--surface0); color: var(--text); border-color: var(--surface1); }
  .ico { width: 16px; text-align: center; color: var(--overlay); }
  .navitem.active .ico { color: var(--accent); }
  .foot { margin-top: auto; display: flex; justify-content: space-between; padding: 8px; font-size: 10.5px; color: var(--overlay); }
  .loopback { color: var(--green); opacity: .8; }
  .content { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  header {
    display: flex; gap: 10px; align-items: center; height: 50px; padding: 0 18px;
    background: var(--mantle); border-bottom: 1px solid var(--surface1);
  }
  .crumb { font-weight: 650; font-size: 14px; }
  .eng { display: flex; gap: 8px; align-items: center; margin-left: 10px; }
  .eng select { max-width: 280px; }
  main { flex: 1; min-height: 0; }
  @media (max-width: 760px) {
    .shell { grid-template-columns: 1fr; grid-template-rows: auto 1fr; }
    nav { flex-direction: row; align-items: center; overflow-x: auto; padding: 6px 10px; }
    .brand { padding: 0 10px 0 0; }
    .section, .foot { display: none; }
    header { flex-wrap: wrap; height: auto; padding: 8px 16px; }
  }
</style>
