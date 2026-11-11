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
    'chevron-down': 'M6 9l6 6 6-6',
    check: 'M5 12l4 4L19 6',
    more: 'M5 12h.01 M12 12h.01 M19 12h.01',
    settings: 'M12 3v3 M12 18v3 M3 12h3 M18 12h3 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2 M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
    arrow: 'M15 6l-6 6 6 6',
    command: 'M8 8h8v8H8z M8 8V5a2 2 0 1 0-2 2h2 M16 8h3a2 2 0 1 0-2-2v2 M16 16v3a2 2 0 1 0 2-2h-2 M8 16H5a2 2 0 1 0 2 2v-2',
    edit: 'M16 3l5 5-12 12H4v-5z M13 6l5 5',
    trash: 'M3 6h18 M9 6V3h6v3 M6 6l1 15h10l1-15 M10 10v7 M14 10v7',
    restore: 'M4 5v6h6 M4 11a8 8 0 1 1 2 7',
    sun: 'M12 2v2 M12 20v2 M2 12h2 M20 12h2 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2 M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
  };
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d={paths[name] || paths.sessions} /></svg>
