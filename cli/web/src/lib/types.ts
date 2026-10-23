// Mirrors the JSON shapes served by cli/web.go and cli/web_control.go.

export type Meta = { version: string; tester: string; engagement: string; workspace: string };

export type Engagement = { name: string; db: string; rows: number };

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

export type SessionEntry = { ts: number; date: string; time: string; cwd: string; command: string; output: string };

export type Session = {
  id: string;
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
  job?: string;
};

export type ShellInfo = { id: string; name: string; kind: string; target: string; active: boolean; index: number };

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

export type ShellOpened = { windowId: string; attached: boolean; attach: string; switch: string };

export type Prefs = {
  theme?: 'dark' | 'light';
  hiddenCols?: string[];
  noiseRules?: string[];
  bulkHidden?: boolean;
  source?: string;
  tester?: string;
  withHeader?: boolean;
  sessAsideCollapsed?: boolean;
  sessAsideWidth?: number;
  navCollapsed?: boolean;
  sessionNames?: Record<string, string>;
  engagement?: string;
};
