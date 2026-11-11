// Mirrors the JSON shapes served by cli/web.go and cli/web_control.go.

export type Meta = { version: string; tester: string; engagement: string; workspace: string; workspaceRoot?: string };

export type Engagement = { name: string; db: string; rows: number; archived?: boolean };

export type ReportRow = {
  id: string;
  ts: number; // ns since epoch
  endTs: number;
  started: string;
  ended: string;
  measure: string;
  command: string;
  targets: string;
  tester: string;
  exit: number;
  noise: boolean;
};

export type SessionEntry = { id: string; sessionId?: string; ts: number; date: string; time: string; cwd: string; command: string; output: string; deleted?: boolean; edited?: boolean };
export type SessionEntryChange = { id: string; command?: string; output?: string; deleted?: boolean; reset?: boolean };
export type EngagementArchive = { file: string; created: number; size: number };

export type Session = {
  id: string;
  captureId?: string;
  displayName?: string;
  deleted?: boolean;
  engagement: string;
  source: string; // "script" | "imported-paste"
  title: string;
  tag: string;
  date: string;
  created: number;
  state?: 'open' | 'ended' | 'imported';
  ended?: number;
  container?: string;
  entries?: SessionEntry[];
};

export type LogHit = {
  sessionId: string;
  title: string;
  index: number;
  ts: number;
  date: string;
  time: string;
  command: string;
  snippet: string;
  inOutput: boolean;
};

export type Route = 'vpn' | 'direct' | 'tor' | 'tor-vpn' | '';

export type ContainerInfo = {
  name: string;
  state: string;
  image: string;
  route: Route;
  vpnFile?: string;
  hasVpn: boolean;
  hasTor: boolean;
  legacy: boolean;
  engagement: string;
  shell?: string;
  session?: string;
  direct: string;
  connect?: string;
  job?: string;
};

export type ShellInfo = { id: string; name: string; kind: string; target: string; active: boolean; index: number; session: string; attach: string; switch: string; direct?: string; location?: 'container' | 'host'; windowId?: string; clients?: number; panes?: number };
export type ContainerState = { containers: ContainerInfo[]; warnings?: string[] };
export type ShellState = { shells: ShellInfo[]; warnings?: string[] };

export type VMConnection = {
  name: string;
  state: string;
  profile: string;
  profileLabel: string;
  ip?: string;
  rdpUser?: string;
  rdpCommand?: string;
  rdpClient?: string;
  rdpReason?: string;
  connect?: string;
};
export type VMConnectionState = { vms: VMConnection[]; warnings?: string[] };


export type Job = {
  id: string;
  kind: string;
  target: string;
  status: 'running' | 'done' | 'failed';
  error?: string;
  output?: string;
  started: number;
  ended?: number;
};

export type Prefs = {
  theme?: 'dark' | 'light' | 'system';
  density?: 'comfortable' | 'compact';
  navWidth?: number;
  trackerWidths?: Record<string, number>;
  outputWrap?: boolean;
  hiddenCols?: string[];
  noiseRules?: string[];
  bulkHidden?: boolean;
  source?: string;
  tester?: string;
  withHeader?: boolean;
  sessAsideCollapsed?: boolean;
  sessAsideWidth?: number;
  timelineWidth?: number;
  replayTheme?: string;
  navCollapsed?: boolean;
  sessionNames?: Record<string, string>;
  engagement?: string;
};

export type RecordingCommand = { id: string; sub: string; sessionId?: string; tag?: string; label: string; t: number; end: number | null; exit: number | null; cwd: string; cmd: string; edited?: boolean };
export type RecordingShell = { sub: string; label: string; start: number | null; bytes: number; big: boolean; finished: boolean; background: boolean };
export type RecordingGroup = { sub: string; sessionId?: string; tag?: string; label: string; command: RecordingCommand | null; commandHit: boolean; hits: {t: number; text: string; screen: boolean}[] };
export type ReplayInfo = { revision?: string; sub: string; width: number; height: number; table: [number, number][]; markers: [number, string][] };
