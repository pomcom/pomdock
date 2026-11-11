// No browser library dependency. Uses Chromium's DevTools protocol and only
// synthetic in-memory API fixtures: no real Docker, tmux, or engagement writes.
import { readFile, writeFile, mkdir, mkdtemp, rm, readdir, access } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { tmpdir, homedir } from 'node:os';
import { join, resolve } from 'node:path';
import assert from 'node:assert/strict';
import { workspaceFixture, installFixture } from './fixtures.mjs';
import { activeColumns, buildItems, toTSV } from '../src/lib/tracker.ts';

const artifacts = process.env.POMDOCK_BROWSER_ARTIFACTS || '/tmp/pomdock-ui-review';
await mkdir(artifacts, { recursive: true });
async function findChromium() {
  const cache = join(homedir(), '.cache/ms-playwright');
  const candidates = [];
  try { for (const entry of await readdir(cache)) if (entry.startsWith('chromium_headless_shell-')) candidates.push(join(cache, entry, 'chrome-headless-shell-linux64/chrome-headless-shell')); } catch {}
  candidates.push('/usr/bin/chromium', '/usr/bin/chromium-browser', '/usr/bin/google-chrome');
  for (const path of candidates) { try { await access(path); return path; } catch {} }
  throw new Error('Set CHROMIUM_PATH to an installed Chromium executable.');
}
const chromePath = process.env.CHROMIUM_PATH || await findChromium();
const profile = await mkdtemp(join(tmpdir(), 'pomdock-design-'));
const chrome = spawn(chromePath, ['--headless', '--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] });
let ws;
const fixtureDocument = join(profile, 'fixture.html');
await writeFile(fixtureDocument, '<!doctype html><html><head><title>pom dock synthetic test</title></head><body>Fixture</body></html>');
const fixtureURL = 'file://' + fixtureDocument;
const deadline = setTimeout(() => { chrome.kill('SIGKILL'); console.error('Browser check timed out'); process.exitCode = 1; }, 180000);
try {
  const endpoint = await new Promise((resolve, reject) => {
    let stderr = '';
    chrome.stderr.on('data', data => { stderr += data; const match = stderr.match(/DevTools listening on (ws:\/\/\S+)/); if (match) resolve(match[1]); });
    chrome.on('error', reject);
  });
  const origin = endpoint.replace('ws:', 'http:').split('/devtools/')[0];
  const pages = await (await fetch(origin + '/json/list')).json();
  ws = new WebSocket(pages.find(p => p.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { ws.addEventListener('open', resolve, { once: true }); ws.addEventListener('error', reject, { once: true }); });
  let seq = 0;
  const pending = new Map(), errors = [];
  ws.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.method === 'Runtime.exceptionThrown') errors.push(message.params.exceptionDetails);
    if (!message.id) return;
    const promise = pending.get(message.id); pending.delete(message.id);
    if (promise) message.error ? promise.reject(new Error(JSON.stringify(message.error))) : promise.resolve(message.result);
  });
  const cdp = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++seq, timer = setTimeout(() => reject(new Error('CDP timeout: ' + method)), 15000);
    pending.set(id, { resolve: result => { clearTimeout(timer); resolve(result); }, reject: error => { clearTimeout(timer); reject(error); } });
    ws.send(JSON.stringify({ id, method, params }));
  });
  const evaluate = async expression => {
    const result = await cdp('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
    return result.result.value;
  };
  const wait = async expression => { for (let i = 0; i < 100; i++) { if (await evaluate(expression)) return; await new Promise(resolve => setTimeout(resolve, 40)); } throw new Error('Timed out: ' + expression); };
  const click = async selector => { await wait(`document.querySelector(${JSON.stringify(selector)})`); await evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`); };
  const input = async (selector, text) => { await evaluate(`(() => { const el = document.querySelector(${JSON.stringify(selector)}); el.value = ${JSON.stringify(text)}; el.dispatchEvent(new Event('input', { bubbles: true })); })()`); };
  const key = async (key, modifiers = 0) => { for (const type of ['keyDown', 'keyUp']) await cdp('Input.dispatchKeyEvent', { type, key, code: key, text: type === 'keyDown' && key === 'Enter' ? '\r' : undefined, modifiers, windowsVirtualKeyCode: ({ Enter: 13, Escape: 27, Tab: 9, ArrowDown: 40, ArrowUp: 38, ArrowLeft: 37, ArrowRight: 39, F2: 113 })[key] }); };
  const screenshot = async name => { const result = await cdp('Page.captureScreenshot', { format: 'png' }); await writeFile(join(artifacts, `${name}.png`), Buffer.from(result.data, 'base64')); };
  const viewport = async (width, height = 960, scale = 1) => { await cdp('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: scale, mobile: false }); await new Promise(resolve => setTimeout(resolve, 100)); };
  function contrast(a, b) {
    const luminance = color => { const values = color.match(/[\d.]+/g).slice(0,3).map(Number).map(v => { v /= 255; return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4; }); return values[0]*.2126 + values[1]*.7152 + values[2]*.0722; };
    const values = [luminance(a),luminance(b)].sort((x,y) => y-x); return (values[0]+.05)/(values[1]+.05);
  }
  async function computedContrast() {
    const samples = await evaluate(`(() => {
      function background(el) { for (let node=el; node; node=node.parentElement) { const color=getComputedStyle(node).backgroundColor; if (!color.endsWith(', 0)') && color !== 'transparent') return color; } return getComputedStyle(document.body).backgroundColor; }
      return [...document.querySelectorAll('.navitem.active, .navitem:hover, .session-name, .session-meta, .command, .output, .save, .export-scope, .tracker .row .ro, .top .muted, .badge, .native-note, .error, .discovery-warning, .vm-meta, .vm-reason, .vm-error, .vm-warning, .timeline-tools label, .timeline-tools .status, .timeline .command-meta, .timeline-command code, .search-hit code, .replay-header time, .replay-help')].filter(el => el.getBoundingClientRect().height && el.textContent.trim()).slice(0,100).map(el => ({ name: el.className, color: getComputedStyle(el).color, background: background(el) }));
    })()`);
    for (const sample of samples) assert(contrast(sample.color, sample.background) >= 4.5, `Computed text contrast: ${JSON.stringify(sample)}`);
  }
  async function mount(dist = process.env.POMDOCK_WEB_DIST || resolve('dist')) {
    const html = await readFile(join(dist, 'index.html'), 'utf8');
    const js = await readFile(join(dist, html.match(/src="([^"]+\.js)"/)[1]), 'utf8');
    const css = await readFile(join(dist, html.match(/href="([^"]+\.css)"/)[1]), 'utf8');
    await evaluate(`location.hash = '/sessions?name=lab-kali&mode=recordings'; (${installFixture.toString()})(${JSON.stringify(workspaceFixture())}); true`);
    const { frameTree } = await cdp('Page.getFrameTree');
    await cdp('Page.setDocumentContent', { frameId: frameTree.frame.id, html: `<html><head><style>${css}</style></head><body><div id="app"></div><script type="module">${js}</script></body></html>` });
    await wait("document.querySelectorAll('.sess').length > 10");
  }
  await cdp('Runtime.enable'); await cdp('Page.enable'); await viewport(1440);
  await cdp('Page.navigate', { url: fixtureURL });
  await wait("location.protocol === 'file:' && document.readyState === 'complete'");
  await evaluate('localStorage.clear()');
  await mount();
  await click('.sess'); await wait("document.querySelectorAll('.execution').length === 12");
  await screenshot('sessions-dark-comfortable-1440');
  if (process.env.POMDOCK_BASELINE) { console.log('Baseline populated screenshot saved.'); }
  else {
    // The first workflow must copy complete output, including collapsed content.
    const beforeCopy = await evaluate("document.querySelector('.scroll[aria-label=\"Recorded commands and output\"]').scrollTop");
    await click('[aria-label="Copy full output"]');
    assert.equal(await evaluate("document.querySelector('.scroll[aria-label=\"Recorded commands and output\"]').scrollTop"), beforeCopy);
    assert.equal(await evaluate('window.__copies.at(-1)'), workspaceFixture().large);
    const before = await evaluate("document.querySelector('.scroll[aria-label=\"Recorded commands and output\"]').scrollTop");
    await click('[aria-label="Find in output"]');
    await input('[aria-label="Search this output"]', 'NEEDLE_AT_END');
    await wait("document.querySelector('.output-find .counter')?.textContent.includes('1 / 1')");
    await key('Escape');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Find in output');
    assert.equal(await evaluate("document.querySelector('.scroll[aria-label=\"Recorded commands and output\"]').scrollTop"), before);
    await evaluate("document.querySelector('.sess').focus()"); await key('ArrowDown');
    assert.equal(await evaluate("document.activeElement === document.querySelectorAll('.sess')[1]"), true);
    assert.equal(await evaluate("document.querySelector('.viewer h1').textContent"), 'Service review');
    await key('ArrowUp');
    await click('[aria-label="Recording actions"]');
    await wait("document.activeElement.getAttribute('role') === 'menuitem'");
    await screenshot('sessions-menu-dark');
    await key('Escape');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Recording actions');
    // Checkbox selection never activates a different recording.
    await click('.session-row:nth-child(2) > input');
    assert.equal(await evaluate("document.querySelector('.viewer h1').textContent"), 'Service review');
    await click('[aria-label="Appearance and density"]');
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === 'Light').click()");
    await key('Escape');
    await screenshot('sessions-light-comfortable-1440');
    await click('[aria-label="Appearance and density"]');
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === 'Compact').click()");
    await key('Escape');
    await viewport(1280); await screenshot('sessions-light-compact-1280');
    for (const width of [1440, 1280, 960, 720, 640, 320]) {
      await viewport(width);
      const dimensions = await evaluate('({ width: innerWidth, document: document.documentElement.scrollWidth })');
      assert(dimensions.document <= width + 1, `Application overflow at ${width}: ${JSON.stringify(dimensions)}`);
      await screenshot(`sessions-reflow-${width}`);
    }
    await viewport(1440);
    // Literal search scope, raw source, and recording identities.
    await input('[aria-label="Search commands and output in current session"]', 'NEEDLE_AT_END');
    await wait("document.querySelector('.searchwrap .counter')?.textContent.includes('1 / 1')");
    await evaluate("const scope = document.querySelector('[aria-label=\"Search scope\"]'); scope.value = 'all'; scope.dispatchEvent(new Event('change', { bubbles: true }));");
    await input('[aria-label="Search all non-archived recordings in this engagement"]', 'NEEDLE_AT_END');
    await wait("document.querySelectorAll('.hit').length > 1");
    assert(await evaluate("document.querySelector('.scope-note').textContent.includes('archived recordings excluded')"));
    await click('.hit'); await wait("document.querySelectorAll('.execution').length === 12");
    await click('[aria-label="Clear search"]');
    await click('[aria-label="Recording actions"]');
    await evaluate("[...document.querySelectorAll('[role=menuitem]')].find(b => b.textContent === 'View original source').click()");
    await wait("document.querySelector('.source-text')");
    await click('[aria-label="Copy original source"]');
    assert((await evaluate('window.__copies.at(-1)')).startsWith('Script started'));
    await evaluate("document.querySelector('.source-heading button').click()");
    await wait("document.querySelector('.command').textContent.includes('nmap')");
    assert.equal(await evaluate("document.querySelector('.command').textContent"), workspaceFixture().recordings[0].entries[0].command);
    // Clipboard fallback restores focus, selection and reading position.
    await evaluate("window.__failClipboard = true; document.querySelector('[aria-label=\"Copy command\"]').focus()");
    await click('[aria-label="Copy command"]');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Copied');
    assert.equal(await evaluate('window.__copies.at(-1)'), workspaceFixture().recordings[0].entries[0].command);
    await evaluate('window.__failClipboard = false');
    assert.equal(await evaluate("window.__requests.filter(r => r.method === 'POST' && r.path.endsWith('/shell')).length"), 0);
    // Live follow, pagination, frozen selection and explicit resume with 1,003 executions.
    const start = performance.now();
    await click('.session-row:nth-child(2) .sess');
    await wait("document.querySelector('.viewer-foot .range')?.textContent.includes('1003')");
    const historyOpenMs = performance.now() - start;
    assert(await evaluate("document.querySelector('.viewer-foot button[aria-pressed]').getAttribute('aria-pressed') === 'true'"));
    await evaluate("document.querySelector('.viewer-foot button[aria-pressed]').click()");
    await evaluate("[...document.querySelectorAll('.viewer-foot button')].find(b => b.textContent === 'Previous').click()");
    await wait("document.querySelectorAll('.execution').length === 100");
    await evaluate("const scroll = document.querySelector('[aria-label=\"Recorded commands and output\"]'); scroll.scrollTop = 240; scroll.dispatchEvent(new Event('scroll')); const pre = document.querySelector('.execution .output'); const range = document.createRange(); range.selectNodeContents(pre); const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range); window.__readSelection = selection.toString(); window.__readTop = scroll.scrollTop; window.__fixture.recordings[1].entries.push({ ...window.__fixture.recordings[1].entries.at(-1), id: 'entry-1003', command: 'printf new-arrival', output: 'New arrival' });");
    await click('[aria-label="Refresh recording"]');
    await wait("document.querySelector('.follow-state')?.textContent.includes('New output')");
    assert.equal(await evaluate('getSelection().toString() === window.__readSelection'), true);
    assert.equal(await evaluate("document.querySelector('[aria-label=\"Recorded commands and output\"]').scrollTop === window.__readTop"), true);
    await evaluate("getSelection().removeAllRanges(); document.querySelector('.viewer-foot button[aria-pressed]').click()");
    await wait("document.querySelector('.viewer-foot .range').textContent.includes('1004')");
    assert(await evaluate("(() => { const el = document.querySelector('[aria-label=\"Recorded commands and output\"]'); return Math.abs(el.scrollHeight - el.clientHeight - el.scrollTop) < 2; })()"));
    await screenshot('sessions-live-follow');
    // Scroll away by more than the bottom tolerance, in a shorter viewport.
    await viewport(1440, 720);
    await evaluate("const el = document.querySelector('[aria-label=\"Recorded commands and output\"]'); el.scrollTop = 0; el.dispatchEvent(new Event('scroll'));");
    await wait("document.querySelector('.viewer-foot button[aria-pressed]').getAttribute('aria-pressed') === 'false'");
    // Tracker selection is temporary, export inclusion is saved independently.
    await evaluate("location.hash = '/tracker?name=lab-kali'");
    await wait("document.querySelectorAll('.tracker tr.row').length === 23");
    // Fields stay directly editable without opening command details.
    assert.equal(await evaluate("document.querySelectorAll('.tracker .measure-input').length"), 23);
    await input('[aria-label="Measure for row 1"]', 'Review exposed service');
    await input('tr.row td.comment input', 'Synthetic worksheet annotation');
    await wait("window.__fixture.tracker.rows['row-0']?.measure === 'Review exposed service' && window.__fixture.tracker.rows['row-0']?.comment === 'Synthetic worksheet annotation'");
    assert.equal(await evaluate("document.querySelector('[aria-label=\"Include row 1 in export\"]').checked"), false);
    await evaluate("document.querySelector('[aria-label=\"Measure for row 1\"]').focus()");
    await wait("document.querySelector('tr.row:focus-within')");
    const editing = await evaluate("(() => { const el=document.activeElement; const row=el.closest('tr'); return { ring:getComputedStyle(el).outlineWidth, color:getComputedStyle(row).backgroundColor, other:getComputedStyle(row.nextElementSibling).backgroundColor, control:getComputedStyle(el).height, target:getComputedStyle(row.querySelector('td.targets input')).height }; })()");
    assert.equal(editing.ring, '2px'); assert.notEqual(editing.color, editing.other); assert.equal(editing.control, editing.target);
    await screenshot('tracker-worksheet-editing');
    await key('Tab');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Affected hosts / targets for row 1');
    await click('tr.row .chev');
    await wait("document.querySelector('.rawcmd')?.textContent.includes('nmap -sV 192.0.2.10')");
    await click('tr.row .chev');
    await wait("!document.querySelector('.detail')");
    await click('[aria-label="Select row 1"]'); await click('[aria-label="Select row 3"]');
    assert.equal(await evaluate("document.querySelector('[aria-label=\"Include row 1 in export\"]').checked"), false);
    await evaluate("[...document.querySelectorAll('.bulk button')].find(b => b.textContent === 'Successful').click()");
    await wait("window.__fixture.tracker.rows['row-0']?.outcome === 'Successful'");
    assert.equal(await evaluate("window.__fixture.tracker.rows['row-2'].outcome"), 'Successful');
    assert.equal(await evaluate("window.__fixture.tracker.rows['row-1']?.outcome"), undefined);
    await evaluate("[...document.querySelectorAll('.selection button')].find(b => b.textContent === 'Include in export').click()");
    await wait("document.querySelector('.export-scope b').textContent === '4'");
    const originalWidth = await evaluate("document.querySelector('[aria-label=\"Resize Measure column\"]').getAttribute('aria-valuenow')");
    await evaluate("document.querySelector('[aria-label=\"Resize Measure column\"]').focus()"); await key('ArrowRight');
    assert.equal(Number(await evaluate("document.querySelector('[aria-label=\"Resize Measure column\"]').getAttribute('aria-valuenow')")), Number(originalWidth) + 16);
    await input('[aria-label="Search tracker commands, measures and targets"]', 'no-such-synthetic-command');
    await wait("document.querySelectorAll('.tracker tr.row').length === 0");
    await evaluate("[...document.querySelectorAll('button')].find(b => b.textContent === 'Copy TSV').click()");
    await wait("window.__fixture.tracker.rows['row-0']?.incl && window.__fixture.tracker.rows['row-2']?.incl");
    await screenshot('tracker-selection');
    const fixture = await evaluate('window.__fixture');
    const expectedItems = buildItems(fixture.rows, fixture.tracker, fixture.tracker.defaults);
    assert.equal(await evaluate('window.__copies.at(-1)'), toTSV(expectedItems, activeColumns(['mitre']), true));
    assert.equal((await evaluate('window.__copies.at(-1)')).split('\n').length, 5);
    await input('[aria-label="Search tracker commands, measures and targets"]', '');
    await click('[aria-label="Export options"]'); await key('Escape');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Export options');
    await evaluate("[...document.querySelectorAll('button')].find(b => b.textContent === 'Columns').click()");
    await wait("document.querySelector('[role=dialog]')");
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === 'MITRE sheet').click()");
    await key('Escape');
    await evaluate("[...document.querySelectorAll('.tracker .sort')].find(b => b.textContent.startsWith('Started')).click()");
    await evaluate("[...document.querySelectorAll('button')].find(b => b.textContent === 'Copy TSV').click()");
    const afterColumns = await evaluate('window.__copies.at(-1)');
    assert(!afterColumns.split('\n')[0].includes('Tester'));
    assert(afterColumns.split('\n')[0].includes('MITRE TTP'));
    assert(afterColumns.split('\n')[1].includes(fixture.rows[0].started));
    await click('[aria-label="Export options"]');
    await evaluate("[...document.querySelectorAll('[role=menuitem]')].find(b => b.textContent === 'Download .tsv').click()");
    await wait('window.__downloads.length === 1');
    assert.equal(await evaluate('window.__downloads[0]'), afterColumns);
    assert.equal(await evaluate('window.__downloadNames[0]'), 'lab-kali-report.tsv');
    // Keyboard command palette yields to dialogs and restores focus.
    await key('F2'); await wait("document.querySelector('[aria-label=\"Find a command\"]')");
    await input('[aria-label="Find a command"]', 'Search current'); await key('Enter');
    await wait("document.activeElement.getAttribute('aria-label') === 'Search tracker commands, measures and targets'");
    // Global scope and exact container/window targets; no real operations occur.
    await evaluate("location.hash = '/containers'"); await wait("document.querySelectorAll('.card').length === 2");
    const connectionRequests = await evaluate('window.__requests.length');
    await click('[title="Copy connect command for lab-kali"]');
    assert.equal(await evaluate('window.__copies.at(-1)'), "pomdock docker attach 'lab-kali'");
    await click('[title="Copy connect command for lab-web"]');
    assert.equal(await evaluate('window.__copies.at(-1)'), "pomdock docker attach 'lab-web'");
    assert(await evaluate("!!document.querySelector('.discovery-warning')"));
    assert(!await evaluate("[...document.querySelectorAll('button')].some(b=>/Show terminal|Open terminal/.test(b.textContent))"));
    assert.equal(await evaluate(`window.__requests.slice(${connectionRequests}).filter(r=>r.method!=='GET').length`),0);
    // Clipboard refusal leaves the selectable command and reports an error;
    // copying never starts or focuses any terminal, including stopped targets.
    await evaluate('window.__failClipboard=true;window.__failClipboardFallback=true');
    const copiesBeforeRefusal=await evaluate('window.__copies.length');
    await click('[title="Copy connect command for lab-kali"]');
    await wait("document.querySelector('[role=status]')?.textContent.includes('Clipboard access failed') || document.body.textContent.includes('Clipboard access failed')");
    assert.equal(await evaluate('window.__copies.length'),copiesBeforeRefusal);
    assert.notEqual(await evaluate("document.querySelector('[title=\"Copy connect command for lab-kali\"]').getAttribute('aria-label')"),'Copied');
    await evaluate('window.__failClipboard=false;window.__failClipboardFallback=false');
    assert(!(await evaluate("document.querySelector('.cards').textContent")).includes('VPN connected'));
    assert(!(await evaluate("document.querySelector('.cards').textContent")).includes('target reachable'));
    await evaluate("[...document.querySelectorAll('button')].find(b => b.textContent === 'New container').click()");
    await wait("document.querySelector('[role=dialog] form')");
    await input('[role=dialog] input[placeholder="acme"]', 'synthetic-validation');
    await evaluate("document.querySelector('[role=dialog] input[value=direct]').click(); window.__createFailure = true;");
    await click('[role=dialog] button[type=submit]');
    await wait("document.querySelector('[role=dialog] [role=alert]')?.textContent.includes('Synthetic container validation failure')");
    assert.equal(await evaluate("document.querySelector('[role=dialog] input[placeholder=acme]').value"), 'synthetic-validation');
    await key('Escape');
    await wait("!document.querySelector('[role=dialog]')");
    assert.equal(await evaluate('document.activeElement.textContent.trim()'), 'New container');
    await evaluate('window.__createFailure = false');
    await evaluate("location.hash = '/shells'"); await wait("document.querySelectorAll('.table-scroll tbody tr').length === 3");
    assert((await evaluate("document.querySelector('.table-scroll').textContent")).includes('Host tmux · existing shell'));
    assert((await evaluate("document.querySelector('.table-scroll').textContent")).includes('2 attached tmux clients · session'));
    assert((await evaluate("document.querySelector('.table-scroll').textContent")).includes('Selected window'));
    assert(!(await evaluate("document.querySelector('.table-scroll').textContent")).includes('Selected in tmux'));
    const shellRequests=await evaluate('window.__requests.length');
    await click('[title="Copy connect command for shell-lab-kali-fixture"]');
    assert.equal(await evaluate('window.__copies.at(-1)'), "pomdock docker attach 'lab-kali' --window '@7'");
    await click('[title="Copy connect command for legacy-lab-web-fixture"]');
    assert.equal(await evaluate('window.__copies.at(-1)'), "tmux attach-session -t '=pomdock-lab-web:@8'");
    await click('[title="Copy direct shell command for lab-web"]');
    assert.equal(await evaluate('window.__copies.at(-1)'), "pomdock docker exec --name 'lab-web'");
    assert.equal(await evaluate(`window.__requests.slice(${shellRequests}).filter(r=>r.method!=='GET').length`),0);
    assert(!await evaluate("document.querySelector('.table-scroll tbody tr:first-child [title^=\"Copy switch\"]')"));
    await click('[aria-label="Close terminal shell-lab-kali-fixture"]'); await wait("document.querySelector('[role=dialog]')");
    assert((await evaluate("document.querySelector('[role=dialog]').textContent")).includes('all running processes in its panes'));
    assert((await evaluate("document.querySelector('[role=dialog]').textContent")).includes('Saved recordings are kept'));
    await key('Escape');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Close terminal shell-lab-kali-fixture');
    assert.equal(await evaluate("window.__requests.filter(r => r.method === 'DELETE').length"), 0);
    assert.equal(await evaluate("window.__requests.filter(r=>r.method==='POST'&&(r.path.endsWith('/shell')||r.path.endsWith('/focus'))).length"),0);
    // Profiled VM connections exist independently of open tmux windows.
    await wait("document.querySelectorAll('.vm-row').length === 7");
    const vmRequests = await evaluate('window.__requests.length');
    await click('[title="Copy RDP command for vm-kali"]');
    assert.equal(await evaluate('window.__copies.at(-1)'),workspaceFixture().vmConnections[0].rdpCommand);
    await evaluate("document.querySelector('[title=\"Copy RDP command for vm-windows\"]').focus()");
    await key('Enter');
    await wait(`window.__copies.at(-1)===${JSON.stringify(workspaceFixture().vmConnections[1].rdpCommand)}`);
    assert.equal(await evaluate('window.__copies.at(-1)'),workspaceFixture().vmConnections[1].rdpCommand);
    assert.equal(await evaluate("document.querySelectorAll('.vm-connections .connect-command').length"),2);
    assert((await evaluate("document.querySelector('.vm-connections').textContent")).includes('VM has not reported an IPv4 address.'));
    assert((await evaluate("document.querySelector('.vm-connections').textContent")).includes('No supported RDP client is installed on the host.'));
    assert((await evaluate("document.querySelector('.vm-connections').textContent")).includes('This profile does not support RDP.'));
    assert((await evaluate("document.querySelector('.vm-connections').textContent")).includes('Assigned profile future-profile is not recognized.'));
    await evaluate("document.querySelector('[title=\"Copy Pomdock RDP command for vm-stopped\"]').closest('details').open=true");
    await click('[title="Copy Pomdock RDP command for vm-stopped"]');
    assert.equal(await evaluate('window.__copies.at(-1)'),"pomdock vm rdp 'vm-stopped'");
    assert.equal(await evaluate(`window.__requests.slice(${vmRequests}).filter(r=>r.method!=='GET').length`),0);
    await evaluate("document.querySelector('.toast.show')?.click();window.__failClipboard=true;window.__failClipboardFallback=true");
    const vmCopies=await evaluate('window.__copies.length');
    await click('[title="Copy RDP command for vm-kali"]');
    await wait("document.querySelector('.toast.show')?.textContent.includes('Clipboard access failed')");
    assert.equal(await evaluate('window.__copies.length'),vmCopies);
    assert.notEqual(await evaluate("document.querySelector('[title=\"Copy RDP command for vm-kali\"]').getAttribute('aria-label')"),'Copied');
    await evaluate('window.__failClipboard=false;window.__failClipboardFallback=false');
    await evaluate("document.querySelector('.vm-connections').scrollIntoView({block:'start'})");
    await screenshot('vm-rdp-connections-populated');
    // No open windows does not hide VMs, and a missing libvirt endpoint cannot
    // discard the existing Docker terminal rows or controls.
    await evaluate('window.__savedShells=window.__fixture.shells;window.__fixture.shells=[]');
    await click('[aria-label="Refresh terminals"]');
    await wait("!document.querySelector('.table-scroll tbody tr') && document.querySelectorAll('.vm-row').length===7");
    await screenshot('vm-rdp-without-tmux-windows');
    await evaluate('window.__fixture.shells=window.__savedShells;window.__vmConnectionFailure=true');
    await click('[aria-label="Refresh terminals"]');
    await wait("document.querySelectorAll('.table-scroll tbody tr').length===3 && document.querySelector('.vm-error')?.textContent.includes('Synthetic libvirt unavailable')");
    assert(await evaluate("!!document.querySelector('[title=\"Copy connect command for shell-lab-kali-fixture\"]')"));
    await screenshot('vm-rdp-discovery-failure');
    await evaluate('window.__vmConnectionFailure=false');
    await click('[aria-label="Refresh terminals"]');
    await wait("!document.querySelector('.vm-error')");
    // A slow VM lookup is shared across repeated refreshes, rather than piling up.
    await evaluate('window.__vmLookupDelay=350;window.__vmPeakActive=0');
    await click('[aria-label="Refresh terminals"]');
    await wait('window.__vmActive===1');
    await click('[aria-label="Refresh terminals"]');
    await wait('window.__vmActive===0');
    assert.equal(await evaluate('window.__vmPeakActive'),1);
    // Unmounted late responses cannot overwrite a newly mounted VM list, even
    // when a fixture deliberately ignores the abort signal.
    await evaluate('window.__vmIgnoreAbort=true');
    await click('[aria-label="Refresh terminals"]');
    await wait('window.__vmActive===1');
    await evaluate("location.hash='/containers'");
    await wait("document.querySelector('.cards')");
    await evaluate("window.__savedVMs=window.__fixture.vmConnections;window.__fixture.vmConnections=[{name:'vm-fresh-response',state:'running',profile:'kali',profileLabel:'Kali',rdpReason:'Synthetic fresh response'}];window.__vmLookupDelay=0;location.hash='/shells'");
    await wait("document.querySelectorAll('.vm-row').length===1 && document.querySelector('.vm-row').textContent.includes('vm-fresh-response') && window.__vmActive===0");
    assert(!(await evaluate("document.querySelector('.vm-connections').textContent")).includes('vm-kali'));
    await evaluate('window.__fixture.vmConnections=window.__savedVMs;window.__vmIgnoreAbort=false');
    await click('[aria-label="Refresh terminals"]');
    await wait("document.querySelectorAll('.vm-row').length===7");
    await evaluate("document.querySelector('.scroll').scrollTop=0");
    // Every populated view, both themes and density modes, at both desktop sizes.
    for (const theme of ['dark', 'light']) for (const density of ['comfortable', 'compact']) {
      await click('[aria-label="Appearance and density"]');
      await evaluate(`[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === '${theme[0].toUpperCase()+theme.slice(1)}').click()`);
      await evaluate(`[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === '${density[0].toUpperCase()+density.slice(1)}').click()`);
      await key('Escape');
      for (const width of [1280, 1440]) for (const view of ['sessions', 'tracker', 'containers', 'shells']) {
        await viewport(width);
        await evaluate(`location.hash = '/${view}${['sessions','tracker'].includes(view) ? '?name=lab-kali' + (view === 'sessions' ? '&mode=recordings' : '') : ''}'`);
        await wait(`document.querySelector('.${view === 'shells' ? 'table-scroll' : view === 'containers' ? 'cards' : view}')`);
        if (view === 'sessions') await wait("document.querySelector('.execution')");
        if (view === 'tracker') await wait("document.querySelector('tr.row')");
        assert(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), `${view} overflow at ${width}, ${theme}, ${density}`);
        const target = await evaluate("(() => { const r=document.querySelector('.navitem:not(.active)').getBoundingClientRect(); return { x:r.left+r.width/2, y:r.top+r.height/2 }; })()");
        await cdp('Input.dispatchMouseEvent', { type: 'mouseMoved', ...target });
        if (view === 'tracker' && width === 1440) {
          await click('[aria-label="Select row 5"]');
          const border = await evaluate("(() => { const row=document.querySelector('tr.selected'); return { foreground:getComputedStyle(row.querySelector('.cell')).borderBottomColor, background:getComputedStyle(row).backgroundColor }; })()");
          assert(contrast(border.foreground, border.background) >= 3, `Selected control boundary: ${theme}: ${JSON.stringify(border)}`);
        }
        await computedContrast();
        await screenshot(`${view}-${theme}-${density}-${width}`);
        if (view === 'shells' && width === 1440) { await wait("document.querySelectorAll('.vm-row').length===7"); await evaluate("document.querySelector('.vm-connections').scrollIntoView({block:'start'})"); await computedContrast(); await screenshot(`vm-rdp-${theme}-${density}-${width}`); await evaluate("document.querySelector('.scroll').scrollTop=0"); }
        if (view === 'sessions' && width === 1440) { await click('nav [aria-label="Select engagement"]'); await wait("document.activeElement.getAttribute('aria-label') === 'Search engagements'"); await key('ArrowDown'); await screenshot(`engagement-menu-${theme}-${density}`); await key('Escape'); }
      }
    }
    // 150% and 200% effective zoom: CSS viewport dimensions scale as well as height.
    for (const [width,height] of [[960,640],[720,480],[640,450],[320,720]]) for (const view of ['sessions','tracker','containers','shells']) {
      await viewport(width,height,width === 960 ? 1.5 : width === 720 || width === 640 ? 2 : 1);
      await evaluate(`location.hash = '/${view}${['sessions','tracker'].includes(view) ? '?name=lab-kali' + (view === 'sessions' ? '&mode=recordings' : '') : ''}'`);
      await wait(`document.querySelector('.${view === 'shells' ? 'table-scroll' : view === 'containers' ? 'cards' : view}')`);
      assert(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), `${view} zoom/reflow overflow at ${width}`);
      await screenshot(`${view}-zoom-reflow-${width}`);
      if (view==='shells') { await wait("document.querySelector('.vm-row')"); await evaluate("document.querySelector('.vm-connections').scrollIntoView({block:'start'})"); assert(await evaluate('document.documentElement.scrollWidth<=innerWidth+1'),`VM RDP reflow overflow at ${width}`); await screenshot(`vm-rdp-zoom-reflow-${width}`); }
    }
    await viewport(320,720);
    await click('[aria-label="Show navigation"]');
    await wait("document.activeElement.closest('nav') !== null");
    await key('Escape');
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'), 'Show navigation');
    // A no-selection state always provides a route back to a visible list.
    await viewport(1440); await evaluate("location.hash = '/sessions?name=lab-web&mode=recordings'");
    await wait("document.querySelector('.welcome')");
    await click('[aria-label="Collapse session list"]');
    await wait("document.querySelector('.sessions.list-hidden')");
    await evaluate("[...document.querySelectorAll('.welcome button')].find(b => b.textContent === 'Browse recordings').click()");
    await wait("document.querySelector('aside[aria-label=\"Session list\"]')");
    // Data failures remain errors, never empty-history claims.
    await evaluate("window.__sessionFailure = true; document.querySelector('[aria-label=\"Refresh recordings\"]').click()");
    await wait("document.querySelector('[role=alert]')?.textContent.includes('Synthetic read failure')");
    await computedContrast();
    await evaluate("window.__sessionFailure = false; document.querySelector('[role=alert] button').click()");
    await wait("!document.querySelector('[role=alert]')");
    // Text-only resizing also retains reflow; technical tables keep their scroll area.
    for (const view of ['sessions','tracker','containers','shells']) {
      await evaluate(`location.hash = '/${view}${['sessions','tracker'].includes(view) ? '?name=lab-kali' + (view === 'sessions' ? '&mode=recordings' : '') : ''}'`);
      await wait(`document.querySelector('.${view === 'shells' ? 'table-scroll' : view === 'containers' ? 'cards' : view}')`);
      await evaluate("document.documentElement.style.fontSize = '200%'");
      assert(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), `${view} text-resize overflow`);
      await screenshot(`${view}-text-200-percent`);
      await evaluate("document.documentElement.style.fontSize = ''");
    }
    // Required focus ring and local-only layout persistence, on a file origin.
    await evaluate("document.querySelector('.navitem.active').focus()"); await key('ArrowRight');
    const ring = await evaluate("(() => { const s=getComputedStyle(document.activeElement); return [s.outlineWidth,s.outlineOffset,s.outlineStyle]; })()");
    assert.deepEqual(ring, ['2px','2px','solid']);
    await click('[aria-label="Appearance and density"]');
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === 'System').click()");
    await cdp('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'dark' }] });
    await wait("document.documentElement.dataset.theme === 'dark'");
    await cdp('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'light' }] });
    await wait("document.documentElement.dataset.theme === 'light'");
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b => b.textContent === 'Light').click()");
    await key('Escape');
    const layout = await evaluate("JSON.parse(localStorage.getItem('pomdock.layout'))");
    assert.equal(layout.theme, 'light'); assert.equal(layout.density, 'compact');
    assert.equal(layout.trackerWidths.measure, Number(originalWidth) + 16);
    assert(Object.keys(layout).every(key => ['theme','density','navWidth','navCollapsed','sessAsideWidth','sessAsideCollapsed','trackerWidths','outputWrap'].includes(key)));
    await cdp('Page.reload');
    await wait("document.readyState === 'complete' && !document.querySelector('#app')");
    await mount();
    await wait("document.documentElement.dataset.theme === 'light' && document.documentElement.dataset.density === 'compact'");
    assert.equal(await evaluate("JSON.parse(localStorage.getItem('pomdock.layout')).trackerWidths.measure"), Number(originalWidth) + 16);
    await writeFile(join(artifacts, 'results.json'), JSON.stringify({ historyExecutions: 1003, historyOpenMs: Math.round(historyOpenMs), outputBytes: Buffer.byteLength(workspaceFixture().large), views: 4, themes: 2, densityModes: 2, desktopWidths: [1280,1440], effectiveZoomWidths: [960,720,640], reflowWidth: 320, errors }, null, 2));
    // Kalico-style timeline, actual bundled replay player, search and curation access.
    const replayScript = await readFile(resolve('src/lib/vendor/asciinema-player.min.js'),'utf8');
    await evaluate(`{ const script=document.createElement('script');script.textContent=${JSON.stringify(replayScript)};document.head.append(script); }`);
    await viewport(1440);
    await evaluate("location.hash='/sessions?name=lab-kali&mode=timeline&q=NEEDLE_AT_END'");
    await wait("document.querySelector('.search-hit')");
    assert.equal(await evaluate("document.querySelector('#recording-search').value"),'NEEDLE_AT_END');
    await input('#recording-search','');
    await wait("document.querySelectorAll('.timeline-command').length > 5");
    await click('.timeline-command');
    await wait("document.querySelector('.player.opened .ap-player')");
    await wait("!document.querySelector('.timeline main [role=status]')");
    assert((await evaluate("document.querySelector('.selected-command').textContent")).includes('nmap'));
    await click('[title="Copy recorded command"]');
    assert.equal(await evaluate('window.__copies.at(-1)'),workspaceFixture().recordings[0].entries[0].command);
    await evaluate("[...document.querySelectorAll('.command-context button')].find(b=>b.textContent==='Command output').click()");
    await wait("document.querySelector('.command-output pre')");
    await click('[title="Copy complete command output"]');
    assert.equal(await evaluate('window.__copies.at(-1)'),workspaceFixture().large);
    assert(await evaluate("window.__requests.some(r=>r.path==='/api/session'&&new URLSearchParams(r.query).get('id')!== 'synthetic-main')"));
    assert(await evaluate("window.__requests.some(r=>r.path==='/api/recordings/cast'&&new URLSearchParams(r.query).get('shell')=== 'synthetic-main')"));
    await input('#recording-search','NEEDLE_AT_END');
    await wait("document.querySelector('.search-hit')");
    await click('.search-hit');
    await wait("document.querySelector('.player.opened')");
    assert(await evaluate("window.__requests.some(r=>r.path==='/api/recordings/search'&&r.query.includes('regex=0'))"));
    const resizeWidth=await evaluate("document.querySelector('[aria-label=\"Resize command timeline\"]').getAttribute('aria-valuenow')");
    await evaluate("document.querySelector('[aria-label=\"Resize command timeline\"]').focus()");
    await key('ArrowRight');
    assert(Number(await evaluate("document.querySelector('[aria-label=\"Resize command timeline\"]').getAttribute('aria-valuenow')"))>Number(resizeWidth));
    for (const width of [1440,1280,640,320]) {
      await viewport(width);
      assert(await evaluate('document.documentElement.scrollWidth <= innerWidth+1'),`timeline overflow at ${width}`);
      await screenshot(`timeline-replay-${width}`);
    }
    await viewport(1440);
    for (const theme of ['dark','light']) for (const density of ['comfortable','compact']) {
      const playerNode=await evaluate("!!document.querySelector('.player.opened .ap-player')");
      await click('[aria-label="Appearance and density"]');
      await evaluate(`[...document.querySelectorAll('[role=dialog] button')].find(b=>b.textContent==='${theme[0].toUpperCase()+theme.slice(1)}').click()`);
      await evaluate(`[...document.querySelectorAll('[role=dialog] button')].find(b=>b.textContent==='${density[0].toUpperCase()+density.slice(1)}').click()`);
      await key('Escape');
      assert(playerNode && await evaluate("!!document.querySelector('.player.opened .ap-player')"));
      for(const width of [1280,1440]) {
        await viewport(width);await computedContrast();await screenshot(`timeline-${theme}-${density}-${width}`);
      }
    }
    await click('[aria-label="Replay colors"]');
    await wait("document.querySelectorAll('[role=menuitemradio]').length===9");
    await screenshot('timeline-replay-palette-menu');
    await evaluate("[...document.querySelectorAll('[role=menuitemradio]')].find(b=>b.textContent.includes('terminal app light')).click()");
    await wait("document.querySelector('.player .asciinema-player-theme-terminal-app-light')");
    assert.equal(await evaluate('document.activeElement.getAttribute("aria-label")'),'Replay colors');
    await input('#recording-search','[');
    await evaluate("document.querySelector('.timeline-tools input[type=checkbox]').click()");
    await wait("document.querySelector('.timeline-tools [role=alert]')?.textContent.includes('Invalid regular expression')");
    await evaluate("[...document.querySelectorAll('.command-context button')].find(b=>b.textContent==='Edit recording').click()");
    await wait("document.querySelector('.execution')");
    assert((await evaluate('location.hash')).includes('mode=recordings'));
    await evaluate("location.hash='/shells';window.__closeFailure=true");
    await wait("document.querySelector('[aria-label=\"Close terminal shell-lab-kali-fixture\"]')");
    await click('[aria-label="Close terminal shell-lab-kali-fixture"]');
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b=>b.textContent==='Close terminal').click()");
    await wait("document.body.textContent.includes('Synthetic window close failure')");
    assert(await evaluate("!!document.querySelector('[role=dialog]')"));
    await evaluate('window.__closeFailure=false');
    await evaluate("[...document.querySelectorAll('[role=dialog] button')].find(b=>b.textContent==='Close terminal').click()");
    await wait("!document.querySelector('[aria-label=\"Close terminal shell-lab-kali-fixture\"]')");
    assert(await evaluate("window.__requests.some(r=>r.method==='DELETE'&&r.path==='/api/shells/docker%3Alab-kali%3A%407')"));
    assert.equal(await evaluate('window.__fixture.recordings.length'), workspaceFixture().recordings.length);
    assert.equal(await evaluate("document.activeElement.getAttribute('aria-label')"),'Refresh terminals');
    assert.equal(await evaluate("window.__requests.filter(r=>r.method==='POST'&&(r.path.endsWith('/shell')||r.path.endsWith('/focus'))).length"),0);
    console.log(`Browser workflows passed; 1,003-execution open: ${Math.round(historyOpenMs)} ms; artifacts: ${artifacts}`);
  }
  assert.deepEqual(errors, []);
} finally {
  clearTimeout(deadline); ws?.close(); chrome.kill('SIGTERM');
  await new Promise(resolve => { if (chrome.exitCode !== null || chrome.signalCode !== null) resolve(); else chrome.once('exit', resolve); });
  await rm(profile, { recursive: true, force: true });
}
