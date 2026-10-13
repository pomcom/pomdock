import type {
  ContainerInfo, Engagement, Job, LogHit, Meta, Prefs, ReportRow, Session, ShellInfo, ShellOpened,
} from './types';

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} };
  if (method !== 'GET') {
    // The server rejects state-changing requests without this header (CSRF guard).
    (init.headers as Record<string, string>)['X-Pomdock'] = '1';
  }
  if (body !== undefined) {
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const res = await fetch(url, init);
  if (!res.ok) {
    let msg = `${method} ${url} failed (${res.status})`;
    try {
      const data = await res.json();
      if (data?.error) msg = data.error;
    } catch {
      if (res.status === 401) msg = 'Not logged in: open the URL printed by `pomdock web` again.';
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

const q = (name: string) => `name=${encodeURIComponent(name)}`;

export const api = {
  meta: () => request<Meta>('GET', '/api/meta'),
  engagements: (refresh = false) =>
    request<{ engagements: Engagement[] }>('GET', `/api/engagements${refresh ? '?refresh=1' : ''}`).then((d) => d.engagements),

  rows: (name: string) => request<{ rows: ReportRow[] }>('GET', `/api/rows?${q(name)}`).then((d) => d.rows),
  trackerState: <T>(name: string) => request<T | null>('GET', `/api/tracker?${q(name)}`),
  saveTrackerState: (name: string, state: unknown) => request<{ ok: boolean }>('PUT', `/api/tracker?${q(name)}`, state),
  prefs: () => request<Prefs | null>('GET', '/api/prefs'),
  savePrefs: (prefs: Prefs) => request<{ ok: boolean }>('PUT', '/api/prefs', prefs),

  sessions: (name: string) => request<{ sessions: Session[] }>('GET', `/api/sessions?${q(name)}`).then((d) => d.sessions),
  session: (name: string, id: string) => request<Session>('GET', `/api/session?${q(name)}&id=${encodeURIComponent(id)}`),
  logSearch: (name: string, query: string) =>
    request<{ hits: LogHit[] }>('GET', `/api/logsearch?${q(name)}&q=${encodeURIComponent(query)}`).then((d) => d.hits),

  containers: () => request<{ containers: ContainerInfo[] }>('GET', '/api/containers').then((d) => d.containers),
  createContainer: (name: string, vpnFile: string, whonix: boolean) =>
    request<Job>('POST', '/api/containers', { name, vpnFile, whonix }),
  startContainer: (name: string) => request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/start`),
  stopContainer: (name: string) => request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/stop`),
  removeContainer: (name: string) => request<Job>('DELETE', `/api/containers/${encodeURIComponent(name)}`),
  recreateContainer: (name: string, vpnFile: string, whonix: boolean) =>
    request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/recreate`, { vpnFile, whonix }),
  openShell: (name: string) => request<ShellOpened>('POST', `/api/containers/${encodeURIComponent(name)}/shell`),
  check: (name: string, tool: 'identity' | 'ports' | 'tor') =>
    request<{ output: string; error?: string }>('POST', `/api/containers/${encodeURIComponent(name)}/check/${tool}`),

  shells: () => request<{ shells: ShellInfo[]; attach: string; switch: string }>('GET', '/api/shells'),
  closeShell: (id: string) => request<{ ok: boolean }>('DELETE', `/api/shells/${encodeURIComponent(id)}`),
  jobs: () => request<{ jobs: Job[] }>('GET', '/api/jobs').then((d) => d.jobs),
  job: (id: string) => request<Job>('GET', `/api/jobs/${encodeURIComponent(id)}`),
};
