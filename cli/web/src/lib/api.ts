import type {
  Engagement, Job, LogHit, Meta, Prefs, ReportRow, Session, SessionEntryChange, EngagementArchive, ContainerState, ShellState, VMConnectionState, RecordingCommand, RecordingShell, RecordingGroup, ReplayInfo,
} from './types';

async function request<T>(method: string, url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const init: RequestInit = { method, headers: {}, signal };
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

  recordingTimeline: (name: string, archived = false) => request<{commands: RecordingCommand[]; shells: RecordingShell[]}>('GET', `/api/recordings/timeline?${q(name)}${archived ? '&archived=1' : ''}`),
  recordingSearch: (name: string, query: string, regex: boolean, matchCase: boolean, archived = false, signal?: AbortSignal) => request<{groups: RecordingGroup[]; truncated: boolean; error?: string}>('GET', `/api/recordings/search?${q(name)}&q=${encodeURIComponent(query)}&regex=${+regex}&case=${+matchCase}${archived ? '&archived=1' : ''}`, undefined, signal),
  replayInfo: (name: string, shell: string, archived = false) => request<ReplayInfo>('GET', `/api/recordings/info?${q(name)}&shell=${encodeURIComponent(shell)}${archived ? '&archived=1' : ''}`),
  replayURL: (name: string, shell: string, archived = false) => `/api/recordings/cast?${q(name)}&shell=${encodeURIComponent(shell)}${archived ? '&archived=1' : ''}`,
  sessions: (name: string, archived = false) =>
    request<{ sessions: Session[] }>('GET', `/api/sessions?${q(name)}${archived ? '&archived=1' : ''}`).then((d) => d.sessions),
  session: (name: string, id: string, archived = false, original = false) =>
    request<Session>('GET', `/api/session?${q(name)}&id=${encodeURIComponent(id)}${archived ? '&archived=1' : ''}${original ? '&original=1' : ''}`),
  sessionSource: (name: string, id: string, archived = false) =>
    request<{ text: string; format: 'transcript' | 'session-json' }>('GET', `/api/session/source?${q(name)}&id=${encodeURIComponent(id)}${archived ? '&archived=1' : ''}`),
  editSession: (name: string, id: string, changes: { displayName?: string; entries?: SessionEntryChange[] }) =>
    request<Session>('PUT', `/api/session/edits?${q(name)}&id=${encodeURIComponent(id)}`, changes),
  deleteSessions: (name: string, ids: string[], deleted: boolean) =>
    request<{ ok: boolean }>('PATCH', `/api/sessions?${q(name)}`, { ids, deleted }),
  engagementArchives: (name: string) => request<{ archives: EngagementArchive[] }>('GET', `/api/engagement/archives?${q(name)}`).then(d => d.archives),
  archiveEngagement: (name: string, passphrase: string) => request<{ ok: boolean; file: string }>('POST', `/api/engagement/archive?${q(name)}`, { passphrase }),
  restoreEngagement: (name: string, file: string, passphrase: string) => request<{ ok: boolean; preferences?: Prefs }>('POST', `/api/engagement/restore?${q(name)}`, { file, passphrase }),
  archiveSession: (name: string, id: string) =>
    request<{ ok: boolean }>('POST', `/api/session/archive?${q(name)}&id=${encodeURIComponent(id)}`),
  unarchiveSession: (name: string, id: string) =>
    request<{ ok: boolean }>('POST', `/api/session/unarchive?${q(name)}&id=${encodeURIComponent(id)}`),
  encryptSession: (name: string, id: string, passphrase: string) =>
    request<{ ok: boolean; archive: string }>('POST', `/api/session/encrypt?${q(name)}&id=${encodeURIComponent(id)}`, { passphrase }),
  encryptedArchives: (name: string) =>
    request<{ archives: string[] }>('GET', `/api/session/encrypted?${q(name)}`).then((d) => d.archives),
  logSearch: (name: string, query: string) =>
    request<{ hits: LogHit[] }>('GET', `/api/logsearch?${q(name)}&q=${encodeURIComponent(query)}`).then((d) => d.hits),

  containerState: () => request<ContainerState>('GET', '/api/containers'),
  containers: () => request<ContainerState>('GET', '/api/containers').then((d) => d.containers),
  createContainer: (name: string, vpnFile: string, whonix: boolean) =>
    request<Job>('POST', '/api/containers', { name, vpnFile, whonix }),
  startContainer: (name: string) => request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/start`),
  stopContainer: (name: string) => request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/stop`),
  removeContainer: (name: string) => request<Job>('DELETE', `/api/containers/${encodeURIComponent(name)}`),
  recreateContainer: (name: string, vpnFile: string, whonix: boolean) =>
    request<Job>('POST', `/api/containers/${encodeURIComponent(name)}/recreate`, { vpnFile, whonix }),
  check: (name: string, tool: 'identity' | 'ports' | 'tor') =>
    request<{ output: string; error?: string }>('POST', `/api/containers/${encodeURIComponent(name)}/check/${tool}`),

  shells: () => request<ShellState>('GET', '/api/shells'),
  vmConnections: (signal?: AbortSignal) => request<VMConnectionState>('GET', '/api/vms/connections', undefined, signal),
  closeShell: (id: string) => request<{ ok: boolean }>('DELETE', `/api/shells/${encodeURIComponent(id)}`),
  jobs: () => request<{ jobs: Job[] }>('GET', '/api/jobs').then((d) => d.jobs),
  job: (id: string) => request<Job>('GET', `/api/jobs/${encodeURIComponent(id)}`),
};
