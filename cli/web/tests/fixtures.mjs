// Deliberately synthetic: TEST-NET addresses and reserved .example domains.
export function workspaceFixture() {
  const created = Date.parse('2026-10-25T08:00:00Z') / 1000;
  const entry = (i) => ({ id: `entry-${i}`, ts: (created + i * 10) * 1e9, date: '2026-10-25', time: new Date((created + i * 10) * 1000).toLocaleTimeString('en-GB', { timeZone: 'Europe/Berlin' }), cwd: '~/workspace', command: `printf 'synthetic result ${i}\\n'`, output: `Synthetic result ${i}\n` });
  const large = Array.from({ length: 600 }, (_, i) => `${String(i).padStart(4, '0')}  192.0.2.10  service-${i}  ${i === 550 ? 'NEEDLE_AT_END' : 'synthetic output'}  ${'x'.repeat(48)}`).join('\n');
  const entries = Array.from({ length: 12 }, (_, i) => entry(i));
  entries[0] = { ...entries[0], command: "nmap -sV \\\n  --script http-title \\\n  -oA results/services 192.0.2.10", cwd: '~/workspace/collected/network/services/2026-10-25/review/very-long-directory-with-a-name-that-must-wrap-cleanly', output: large };
  entries[1] = { ...entries[1], ts: 0, date: '', time: '', cwd: '', command: 'printf "<script>untrusted text</script>\\n"', output: '<script>untrusted text</script>\nNo HTML executes.\n' };
  entries[2] = { ...entries[2], command: 'mkdir -p results', output: '' };
  entries[4] = { ...entries[4], command: 'printf long-json-line', output: JSON.stringify({ path: '/synthetic/long/path/'.repeat(12), detail: 'x'.repeat(2000) }) };
  entries[3] = { ...entries[3], command: 'legacy-command', output: null };
  const recordings = [
    { id: 'capture-proxy-a7f2#0', engagement: 'lab-kali', source: 'script', title: 'Recorded session capture-proxy-a7f2', displayName: 'Service review', tag: '', created, date: '2026-10-25', state: 'ended', ended: created + 1800, entries },
    { id: 'capture-live-391b#0', engagement: 'lab-kali', source: 'script', title: 'Recorded session capture-live-391b', tag: '', created, date: '2026-10-25', state: 'open', entries: Array.from({ length: 1003 }, (_, i) => entry(i)) },
    { id: 'import-legacy-c48e', engagement: 'lab-kali', source: 'imported-paste', title: 'Imported: terminal-notes.txt', tag: '', created: 0, date: '', state: 'imported', entries: entries.slice(1, 4) },
    ...Array.from({ length: 18 }, (_, i) => ({ id: `capture-secondary-${String(i).padStart(4, '0')}#0`, engagement: 'lab-kali', source: 'script', title: `Recorded session capture-secondary-${String(i).padStart(4, '0')}`, tag: i === 0 ? 'Proxy checks' : '', created: created - i * 3600, date: '2026-10-25', state: i % 3 === 0 ? 'open' : 'ended', entries: entries.slice(0, 2) })),
  ];
  const rows = Array.from({ length: 24 }, (_, i) => ({ id: `row-${i}`, ts: (created + i * 60) * 1e9, endTs: (created + i * 60 + 12) * 1e9, started: `25.10.2026 09:${String(i).padStart(2, '0')}:00`, ended: `25.10.2026 09:${String(i).padStart(2, '0')}:12`, command: i % 2 ? `curl --head https://service-${i}.lab.example` : `nmap -sV 192.0.2.${10 + i}`, measure: i % 2 ? `Review HTTP headers ${i}` : `nmap -sV 192.0.2.${10 + i}`, targets: `192.0.2.${10 + i}`, tester: 'DEMO', exit: i === 2 ? 1 : 0, noise: i === 23 }));
  return {
    meta: { version: 'Design fixture', tester: 'DEMO', engagement: 'lab-kali', workspace: 'pomdock' },
    prefs: { theme: 'dark', density: 'comfortable', navCollapsed: false, sessAsideCollapsed: false, withHeader: true },
    engagements: [{ name: 'lab-kali', db: 'synthetic', rows: rows.length }, { name: 'lab-web', db: 'synthetic', rows: 0 }, { name: 'long-engagement-name-for-keyboard-and-reflow-review', db: 'synthetic', rows: 0 }],
    recordings, rows,
    tracker: { version: 1, defaults: { tester: 'DEMO', source: '192.0.2.5' }, rows: { 'row-1': { incl: true }, 'row-3': { incl: true } }, merged: [] },
    containers: [
      { name: 'lab-kali', engagement: 'lab-kali', state: 'running', image: 'pcm-kali-pentest:fixture', route: 'vpn', hasVpn: true, hasTor: false, legacy: false, shell: '@7', session: 'pomdock', connect: "pomdock docker attach 'lab-kali'", direct: "pomdock docker exec --name 'lab-kali'" },
      { name: 'lab-web', engagement: 'lab-web', state: 'exited', image: 'pcm-kali-pentest:fixture', route: 'direct', hasVpn: false, hasTor: false, legacy: false, connect: "pomdock docker attach 'lab-web'", direct: "pomdock docker exec --name 'lab-web'" },
    ],
    vmConnections: [
      { name: 'vm-kali', state: 'running', profile: 'kali', profileLabel: 'Kali', ip: '192.0.2.44', rdpUser: 'kali', rdpClient: 'xfreerdp3', rdpCommand: "'xfreerdp3' '/v:192.0.2.44' '/u:kali' '/dynamic-resolution' '/gfx:avc444' '+clipboard' '/cert:tofu' '/log-level:ERROR'", connect: "pomdock vm rdp 'vm-kali'" },
      { name: 'vm-windows', state: 'running', profile: 'windows-server-2022', profileLabel: 'Windows Server 2022', ip: '198.51.100.24', rdpUser: 'Administrator', rdpClient: 'xfreerdp3', rdpCommand: "'xfreerdp3' '/v:198.51.100.24' '/u:Administrator' '/dynamic-resolution' '/gfx:avc444' '+clipboard' '/cert:tofu' '/log-level:ERROR'", connect: "pomdock vm rdp 'vm-windows'" },
      { name: 'vm-stopped', state: 'shut off', profile: 'kali', profileLabel: 'Kali', rdpUser: 'kali', rdpReason: 'VM is shut off; no direct RDP command is available.', connect: "pomdock vm rdp 'vm-stopped'" },
      { name: 'vm-no-ip', state: 'running', profile: 'kali', profileLabel: 'Kali', rdpUser: 'kali', rdpReason: 'VM has not reported an IPv4 address.', connect: "pomdock vm rdp 'vm-no-ip'" },
      { name: 'vm-no-rdp', state: 'running', profile: 'ubuntu-lts', profileLabel: 'Ubuntu 24.04 LTS', ip: '203.0.113.4', rdpReason: 'This profile does not support RDP.' },
      { name: 'vm-no-client', state: 'running', profile: 'kali', profileLabel: 'Kali', ip: '203.0.113.45', rdpUser: 'kali', rdpReason: 'No supported RDP client is installed on the host.', connect: "pomdock vm rdp 'vm-no-client'" },
      { name: 'vm-future-profile', state: 'running', profile: 'future-profile', profileLabel: 'Unknown profile', rdpReason: 'Assigned profile future-profile is not recognized.' },
    ],
    vmWarnings: ['Synthetic warning: one VM could not be inspected.'],
    containerWarnings: ['Synthetic warning: tmux details for lab-web are unavailable.'],
    shellWarnings: ['Synthetic warning: one stopped container could not be inspected.'],
    shells: [
      { id: 'docker:lab-kali:@7', windowId: '@7', location: 'container', name: 'shell-lab-kali-fixture', kind: 'docker', target: 'lab-kali', active: true, index: 1, session: 'pomdock', clients: 2, panes: 3, attach: "pomdock docker attach 'lab-kali' --window '@7'", switch: '', direct: "pomdock docker exec --name 'lab-kali'" },
      { id: '@8', windowId: '@8', location: 'host', name: 'legacy-lab-web-fixture', kind: 'docker', target: 'lab-web', active: false, index: 2, session: 'pomdock-lab-web', attach: "tmux attach-session -t '=pomdock-lab-web:@8'", switch: "tmux switch-client -t '=pomdock-lab-web:@8'", direct: "pomdock docker exec --name 'lab-web'" },
      { id: '@9', windowId: '@9', location: 'host', name: 'existing-vm-fixture', kind: 'vm', target: 'vm-synthetic', active: false, index: 3, session: 'pomdock', clients: 0, panes: 1, attach: "tmux attach-session -t '=pomdock:@9'", switch: "tmux switch-client -t '=pomdock:@9'" },
    ],
    large,
  };
}

export function installFixture(fixture) {
  window.__fixture = fixture;
  window.__requests = [];
  window.__copies = [];
  window.__downloads = []; window.__downloadNames = [];
  const createURL = URL.createObjectURL.bind(URL);
  URL.createObjectURL = blob => { const url = createURL(blob); blob.text().then(text => window.__downloads.push(text)); return url; };
  const anchorClick = HTMLAnchorElement.prototype.click;
  HTMLAnchorElement.prototype.click = function () { if (this.download) window.__downloadNames.push(this.download); else anchorClick.call(this); };
  window.__sessionFailure = false;
  window.__failClipboard = false; window.__failClipboardFallback = false; window.__closeFailure = false; window.__vmConnectionFailure = false; window.__vmLookupDelay = 0; window.__vmActive = 0; window.__vmPeakActive = 0; window.__vmIgnoreAbort = false;
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => { if (window.__failClipboard) throw new Error('Fixture clipboard denied'); window.__copies.push(text); } } });
  document.execCommand = command => { if (command === 'copy') { if (window.__failClipboardFallback) return false; window.__copies.push(document.activeElement.value); return true; } return false; };
  window.fetch = async (url, options = {}) => {
    const parsed = new URL(url, 'http://fixture.local');
    const path = parsed.pathname, method = options.method || 'GET', body = options.body ? JSON.parse(options.body) : undefined;
    window.__requests.push({ path, method, body, query: parsed.search });
    let value;
    if (path === '/api/meta') value = fixture.meta;
    else if (path === '/api/prefs') { if (method === 'PUT') fixture.prefs = body; value = method === 'GET' ? fixture.prefs : { ok: true }; }
    else if (path === '/api/engagements') value = { engagements: fixture.engagements };
    else if (path === '/api/recordings/timeline') {
      const recording = fixture.recordings[0];
      value = { commands: parsed.searchParams.get('archived') ? [] : recording.entries.filter(e=>e.ts).map((e,i)=>({id:e.id,sub:'synthetic-main',sessionId:recording.id,tag:'recon',label:'tmux lab-kali_0_0',t:e.ts/1e6,end:e.ts/1e6+500,exit:i===2?1:0,cwd:e.cwd,cmd:e.command})), shells:[{sub:'synthetic-main',sessionId:recording.id,tag:'recon',label:'tmux lab-kali_0_0',start:recording.created*1000,bytes:4096,big:false,finished:true,background:false}] };
    }
    else if (path === '/api/recordings/search') {
      const recording=fixture.recordings[0],q=parsed.searchParams.get('q');
      const re=parsed.searchParams.get('regex')==='1';
      let pattern;try{pattern=new RegExp(re?q:q.replace(/[.*+?^${}()|[\]\\]/g,'\\$&'),parsed.searchParams.get('case')==='1'?'':'i');}catch{value={groups:[],error:'Invalid regular expression',truncated:false};}
      if(!value)value={groups:recording.entries.filter(e=>pattern.test(e.command)||pattern.test(e.output||'')).map(e=>({sub:'synthetic-main',sessionId:recording.id,tag:'recon',label:'tmux lab-kali_0_0',command:{id:e.id,sub:'synthetic-main',sessionId:recording.id,tag:'recon',label:'tmux lab-kali_0_0',t:e.ts/1e6,end:e.ts/1e6+500,exit:0,cwd:e.cwd,cmd:e.command},commandHit:pattern.test(e.command),hits:(e.output||'').split('\n').filter(line=>pattern.test(line)).map(text=>({t:recording.created*1000+200,text,screen:false}))})),truncated:false};
    }
    else if (path === '/api/recordings/info') value={sub:parsed.searchParams.get('shell'),width:100,height:30,table:[[0,fixture.recordings[0].created*1000]],markers:fixture.recordings[0].entries.map((e,i)=>[i*10,e.command])};
    else if (path === '/api/recordings/cast') {
      const frames=[{version:2,width:100,height:30,timestamp:fixture.recordings[0].created},[0,'o','\u001b[32m$ nmap -sV 192.0.2.10\u001b[0m\r\n'],[.2,'o','Synthetic terminal replay\r\nNEEDLE_AT_END\r\n']];
      return new Response(frames.map(f=>JSON.stringify(f)).join('\n')+'\n',{headers:{'Content-Type':'text/plain'}});
    }
    else if (path === '/api/sessions') {
      if (window.__sessionFailure) return new Response(JSON.stringify({ error: 'Synthetic read failure' }), { status: 503 });
      value = { sessions: parsed.searchParams.get('name') === 'lab-kali' && !parsed.searchParams.has('archived') ? fixture.recordings.map(({ entries, ...meta }) => meta) : [] };
    } else if (path === '/api/session') value = fixture.recordings.find(s => s.id === parsed.searchParams.get('id'));
    else if (path === '/api/session/source') value = { text: 'Script started\n❯ printf synthetic\nsynthetic\nScript done\n', format: 'transcript' };
    else if (path === '/api/logsearch') {
      const q = (parsed.searchParams.get('q') || '').toLowerCase();
      value = { hits: fixture.recordings.flatMap(s => s.entries.flatMap((e, index) => (e.command + ' ' + (e.output || '')).toLowerCase().includes(q) ? [{ sessionId: s.id, title: s.title, index, ts: e.ts, date: e.date, time: e.time, command: e.command, snippet: (e.output || '').slice(-250), inOutput: (e.output || '').toLowerCase().includes(q) }] : [])) };
    } else if (path === '/api/rows') value = { rows: parsed.searchParams.get('name') === 'lab-kali' ? fixture.rows : [] };
    else if (path === '/api/tracker') { if (method === 'PUT') fixture.tracker = body; value = method === 'GET' ? fixture.tracker : { ok: true }; }
    else if (path === '/api/containers') { if (method === 'POST' && window.__createFailure) return new Response(JSON.stringify({ error: 'Synthetic container validation failure' }), { status: 400 }); value = method === 'GET' ? { containers: fixture.containers, warnings: fixture.containerWarnings } : { id: 'create-fixture', kind: 'create', target: body.name, status: 'running', started: 0 }; }
    else if (path === '/api/vms/connections') {
      if (window.__vmConnectionFailure) return new Response(JSON.stringify({ error: 'Synthetic libvirt unavailable' }), { status: 503 });
      window.__vmActive++; window.__vmPeakActive = Math.max(window.__vmPeakActive, window.__vmActive);
      const result = { vms: structuredClone(fixture.vmConnections), warnings: fixture.vmWarnings };
      try {
        if (window.__vmLookupDelay) await new Promise((resolve, reject) => {
          const timer = setTimeout(resolve, window.__vmLookupDelay);
          if (!window.__vmIgnoreAbort) options.signal?.addEventListener('abort', () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); }, { once: true });
        });
        value = result;
      } finally { window.__vmActive--; }
    }
    else if (path === '/api/shells') value = { shells: fixture.shells, warnings: fixture.shellWarnings };
    else if (path.startsWith('/api/shells/') && method === 'DELETE') {
      if (window.__closeFailure) return new Response(JSON.stringify({ error: 'Synthetic window close failure' }), { status: 503 });
      fixture.shells = fixture.shells.filter(shell => shell.id !== decodeURIComponent(path.slice('/api/shells/'.length))); value = { ok: true };
    }
    else if (path === '/api/jobs') value = { jobs: [] };
    else if (path.endsWith('/archives') || path.endsWith('/encrypted')) value = { archives: [] };
    else value = { ok: true };
    return new Response(JSON.stringify(value ?? null), { headers: { 'Content-Type': 'application/json' } });
  };
}
