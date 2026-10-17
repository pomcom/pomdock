<script lang="ts">
  import { filterHelp, HELP, ROUTES_HELP } from '../help';
  import CopyButton from './CopyButton.svelte';
  import RouteBadge from './RouteBadge.svelte';

  let query = $state('');
  const sections = $derived(filterHelp(HELP, query));
</script>

<div class="view">
  <div class="bar">
    <input class="search" bind:value={query} placeholder="Filter commands…" />
    <span class="grow"></span>
    <span class="muted">Copy a command and replace the UPPERCASE placeholders.</span>
  </div>

  <div class="scroll">
    {#if !query.trim()}
      <section>
        <h2>Routes</h2>
        <div class="routes">
          {#each ROUTES_HELP as r (r.route)}
            <div class="route"><RouteBadge route={r.route} /><span>{r.desc}</span></div>
          {/each}
        </div>
      </section>
    {/if}

    {#each sections as s (s.title)}
      <section>
        <h2>{s.title} <span class="where where-{s.where}">{s.where === 'host' ? 'on the host' : 'in the container'}</span></h2>
        {#if s.intro}<p class="intro">{s.intro}</p>{/if}
        <div class="cmds">
          {#each s.commands as c (c.cmd)}
            <div class="row">
              <code class="cmd">{c.cmd}</code>
              <span class="desc">{c.desc}</span>
              <CopyButton text={c.cmd} title="copy command" />
            </div>
          {/each}
        </div>
      </section>
    {:else}
      <div class="empty">No command matches “{query.trim()}”.</div>
    {/each}
  </div>
</div>

<style>
  .view { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .search { width: 320px; max-width: 60vw; background: var(--base); }
  .muted { color: var(--overlay); font-size: 11px; }
  .scroll { flex: 1; overflow: auto; padding: 8px 22px 40px; }
  section { max-width: 1100px; margin-top: 18px; }
  h2 { font-size: 13.5px; margin: 0 0 6px; display: flex; gap: 10px; align-items: center; }
  .where { font-size: 10.5px; font-weight: 600; border-radius: 20px; padding: 1px 8px; }
  .where-host { background: var(--surface0); color: var(--subtext); }
  .where-container { background: color-mix(in srgb, var(--accent) 20%, transparent); color: var(--accent); }
  .intro { color: var(--subtext); font-size: 12px; margin: 0 0 8px; line-height: 1.5; }
  .cmds { border: 1px solid var(--surface1); border-radius: 10px; overflow: hidden; }
  .row {
    display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr) auto; gap: 14px; align-items: center;
    padding: 7px 12px; border-bottom: 1px solid var(--surface0); background: var(--mantle);
  }
  .row:last-child { border-bottom: none; }
  .cmd { background: none; padding: 0; font-size: 12.5px; color: var(--text); overflow-wrap: anywhere; }
  .desc { color: var(--subtext); font-size: 12px; }
  .routes { display: grid; gap: 6px; }
  .route { display: flex; gap: 10px; align-items: center; font-size: 12px; color: var(--subtext); }
  @media (max-width: 760px) {
    .row { grid-template-columns: 1fr auto; }
    .desc { grid-column: 1; }
    .muted { display: none; }
  }
</style>
