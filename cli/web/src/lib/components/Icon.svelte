<script lang="ts">
  let { name, size = 16 }: { name: string; size?: number } = $props();
  const paths: Record<string, string> = {
    tracker: 'M4 4h16v16H4z M8 8h8 M8 12h8 M8 16h5',
    sessions: 'M4 5h16v14H4z M8 9l3 3-3 3 M13 15h4',
    containers: 'M12 3l9 5-9 5-9-5z M3 8v9l9 5 9-5V8 M12 13v9',
    shells: 'M4 6h16v13H4z M7 10l3 3-3 3 M12 16h5',
    panel: 'M3 4h18v16H3z M9 4v16',
    search: 'M21 21l-5-5 M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
    refresh: 'M20 7v5h-5 M4 17v-5h5 M6 6a8 8 0 0 1 13 3 M18 18a8 8 0 0 1-13-3',
    copy: 'M8 8h12v13H8z M16 8V3H3v13h5',
    sun: 'M12 2v2 M12 20v2 M2 12h2 M20 12h2 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2 M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
  };
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d={paths[name] || paths.sessions} /></svg>
