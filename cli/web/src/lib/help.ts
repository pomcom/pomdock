// Content of the Help view: the commands people reach for, grouped by where
// they run. Keep in sync with pentest.sh, scripts/pomsession and scripts/pomrdp.

export type HelpCommand = { cmd: string; desc: string };
export type HelpSection = { title: string; where: 'host' | 'container'; intro?: string; commands: HelpCommand[] };

export const HELP: HelpSection[] = [
  {
    title: 'Containers',
    where: 'host',
    intro: 'Each --name gets its own container, loot dir (~/pentest/NAME) and Atuin history. exec attaches if running, starts if stopped, creates if missing.',
    commands: [
      { cmd: 'pomdock docker exec --name NAME', desc: 'Kali shell, direct internet' },
      { cmd: 'pomdock docker exec --name NAME --vpn ~/vpn/mullvad.conf', desc: 'all traffic via gluetun VPN, kill switch (.conf WireGuard / .ovpn OpenVPN)' },
      { cmd: 'pomdock docker exec --name NAME --lab ~/vpn/lab.ovpn', desc: 'HTB / THM lab VPN inside the container; internet stays direct' },
      { cmd: 'pomdock docker exec --name NAME --whonix', desc: 'all traffic via Tor' },
      { cmd: 'pomdock docker exec --name NAME --whonix --vpn FILE', desc: 'Tor over VPN' },
      { cmd: 'pomdock docker status', desc: 'container, VPN and Tor state of the default container (named ones: Containers tab)' },
      { cmd: 'pomdock docker logs --name NAME', desc: 'gluetun / Tor sidecar logs' },
      { cmd: 'pomdock docker stop --name NAME', desc: 'stop container and sidecars' },
      { cmd: 'pomdock docker rm --name NAME', desc: 'remove container and sidecars (asks before touching the loot dir)' },
      { cmd: 'pomdock docker build', desc: 'rebuild the Kali image (needed after tool or helper changes)' },
      { cmd: 'pomdock tui', desc: 'terminal dashboard in the pomdock tmux workspace' },
    ],
  },
  {
    title: 'Recording & sessions',
    where: 'container',
    intro: 'Every shell opened through pomdock is recorded to ~/pentest/NAME/sessions/. Tags split one recording into sessions you can filter in the Sessions tab.',
    commands: [
      { cmd: 'pdsession recon', desc: 'start a new session tagged "recon" (short for pomsession start)' },
      { cmd: 'pomsession start TAG', desc: 'same, long form' },
      { cmd: 'pomsession status', desc: 'is this shell being recorded, and where to' },
      { cmd: 'pomhelp', desc: 'list the session helpers' },
    ],
  },
  {
    title: 'Lab (HackTheBox / TryHackMe)',
    where: 'container',
    intro: 'Needs a container created with --lab (route "Lab"). GUI windows open on your desktop over X11; their traffic goes through the lab VPN.',
    commands: [
      { cmd: "ip -4 -br addr show tun0", desc: 'your lab IP, for reverse shells and listeners' },
      { cmd: "pomrdp -u Administrator -p 'PASSWORD' 10.10.10.5", desc: 'RDP to a lab Windows host (cert ignored, clipboard on)' },
      { cmd: 'pomrdp -d CORP -u alice -H NTHASH 10.10.10.5', desc: 'RDP with pass-the-hash (Restricted Admin mode)' },
      { cmd: 'pomrdp -u alice 10.10.10.5 -- /size:1600x900', desc: 'extra xfreerdp options after --' },
      { cmd: 'firefox &', desc: 'browser for lab web apps' },
      { cmd: '\\\\tsclient\\loot', desc: 'inside the RDP session: the container\'s ~/pentest (loot dir)' },
    ],
  },
  {
    title: 'Lab VPN troubleshooting',
    where: 'host',
    commands: [
      { cmd: 'pomdock docker exec --name NAME', desc: 'restarts the lab OpenVPN if it died and prints the lab IP' },
      { cmd: 'docker exec -u root NAME cat /tmp/lab-openvpn.log', desc: 'OpenVPN log of the lab tunnel' },
    ],
  },
  {
    title: 'Web UI',
    where: 'host',
    intro: 'Binds 127.0.0.1 only. Archive in the Sessions tab hides a recording (restorable); Delete removes it for good. Neither touches Atuin history or the tracker.',
    commands: [
      { cmd: 'pomdock web --name NAME', desc: 'open the web UI on an engagement' },
      { cmd: 'pomdock web --token-file ~/.config/pomdock/web-token', desc: 'keep the login token across restarts' },
      { cmd: 'pomdock web import scrollback.txt --name NAME', desc: 'turn a pasted terminal scrollback into a session' },
    ],
  },
];

export const ROUTES_HELP: { route: 'direct' | 'vpn' | 'lab' | 'tor' | 'tor-vpn'; desc: string }[] = [
  { route: 'direct', desc: 'no tunnel, your own IP' },
  { route: 'vpn', desc: 'everything via gluetun; blocked if the VPN drops' },
  { route: 'lab', desc: 'lab network via OpenVPN in the container, internet direct, no kill switch' },
  { route: 'tor', desc: 'everything via Tor' },
  { route: 'tor-vpn', desc: 'Kali → Tor → VPN' },
];

/** Sections with only the commands matching q (in command or description). */
export function filterHelp(sections: HelpSection[], q: string): HelpSection[] {
  const needle = q.trim().toLowerCase();
  if (!needle) return sections;
  return sections
    .map((s) => ({
      ...s,
      commands: s.commands.filter((c) => c.cmd.toLowerCase().includes(needle) || c.desc.toLowerCase().includes(needle)),
    }))
    .filter((s) => s.commands.length > 0);
}
