import assert from 'node:assert/strict';
import { test } from 'node:test';
import { api } from './api.ts';

test('container discovery retains partial warnings and keeps the array API compatible', async t => {
  const state = { containers: [{ name: 'lab', connect: "pomdock docker attach 'lab'" }], warnings: ['Synthetic partial discovery'] };
  const calls: { url: string; method?: string }[] = [];
  t.mock.method(globalThis, 'fetch', async (url: string, options: RequestInit) => {
    calls.push({ url, method: options.method });
    return new Response(JSON.stringify(state));
  });
  assert.deepEqual(await api.containerState(), state);
  assert.deepEqual(await api.containers(), state.containers);
  assert(calls.every(call => call.url === '/api/containers' && call.method === 'GET'));
});

test('shell discovery preserves actual tmux scope, counts and optional warning compatibility', async t => {
  const shell = { id: 'docker:lab:@4', windowId: '@4', location: 'container', clients: 0, panes: 2, attach: "pomdock docker attach 'lab' --window '@4'", switch: '' };
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ shells: [shell] })));
  const result = await api.shells();
  assert.deepEqual(result.shells, [shell]);
  assert.equal(result.warnings, undefined);
  assert.equal(result.shells[0].clients, 0);
});

test('confirmed close addresses exactly one opaque container window', async t => {
  let call: { url: string; options: RequestInit } | undefined;
  t.mock.method(globalThis, 'fetch', async (url: string, options: RequestInit) => {
    call = { url, options };
    return new Response(JSON.stringify({ ok: true }));
  });
  await api.closeShell('docker:lab:@4');
  assert.equal(call?.url, '/api/shells/docker%3Alab%3A%404');
  assert.equal(call?.options.method, 'DELETE');
  assert.equal((call?.options.headers as Record<string, string>)['X-Pomdock'], '1');
});

test('VM RDP commands and unavailable reasons are read without starting or probing guests', async t => {
  const state = { vms: [
    { name: 'vm-kali', state: 'running', profile: 'kali', profileLabel: 'Kali', ip: '192.0.2.44', rdpUser: 'kali', rdpClient: 'xfreerdp3', rdpCommand: "xfreerdp3 '/v:192.0.2.44' '/u:kali'", connect: "pomdock vm rdp 'vm-kali'" },
    { name: 'vm-stopped', state: 'shut off', profile: 'kali', profileLabel: 'Kali', rdpReason: 'VM is shut off.', connect: "pomdock vm rdp 'vm-stopped'" },
  ], warnings: ['Synthetic partial VM discovery'] };
  const calls: { url: string; options: RequestInit }[] = [];
  t.mock.method(globalThis, 'fetch', async (url: string, options: RequestInit) => { calls.push({ url, options }); return new Response(JSON.stringify(state)); });
  const controller = new AbortController();
  const result = await api.vmConnections(controller.signal);
  assert.deepEqual(result, state);
  assert.equal(result.vms[1].rdpCommand, undefined);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/vms/connections');
  assert.equal(calls[0].options.method, 'GET');
  assert.equal(calls[0].options.signal, controller.signal);
  assert.equal(calls[0].options.body, undefined);
});

test('VM discovery failure does not prevent independent Docker terminal discovery', async t => {
  t.mock.method(globalThis, 'fetch', async (url: string) => url === '/api/vms/connections'
    ? new Response(JSON.stringify({ error: 'Synthetic libvirt unavailable' }), { status: 503 })
    : new Response(JSON.stringify({ shells: [{ id: 'docker:lab:@4', location: 'container' }] })));
  await assert.rejects(api.vmConnections(), /Synthetic libvirt unavailable/);
  assert.equal((await api.shells()).shells[0].id, 'docker:lab:@4');
});
