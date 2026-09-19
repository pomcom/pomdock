# Sessions & reporting

Every shell opened via `pomdock docker exec` (or a Shells-tab window) is recorded with
`script` into `~/pentest/<engagement>/sessions/`, on a host volume so recordings survive
`docker rm`. Recording is armed by the `POMDOCK_ENGAGEMENT` variable the container's
zshrc checks, so it never fires on the host or in a plain `docker exec`. A shell opened
before the container picked up the hook is not recorded, so open a fresh one.

## Tagging sub-sessions

Inside a recorded shell, split a long capture into labelled sessions:

```
pomsession start recon      # begin a session tagged "recon"
pomsession start exploit    # everything after this becomes a new "exploit" session
pdsession recon             # short alias for the same thing
pomsession help             # (or pomhelp) list the helpers and show recording state
```

Each `pomsession start` writes a marker the report viewer splits on, so one capture can
hold several tagged sessions you filter by in the UI. It warns instead of tagging if the
shell is not being recorded.

## Browsing and exporting

```bash
pomdock report                                         # or: pomdock tracker
pomdock report --name acme                          # open an engagement directly
pomdock report import scrollback.txt --name acme    # import a pasted log
```

The UI has two tabs:

- **Tracker**: the engagement's Atuin history as team-report rows, copyable as TSV.
- **Sessions**: recorded captures and imported logs, searchable across commands and
  output.

It reads the loot dir read-only and never touches the container image.

## How reading a recording works

A raw `script` log is full of the line editor's redraw noise. Opening a recording
reconstructs each typed command (replaying `\r`, `\b`, and cursor moves), splits the
transcript at `pomsession` markers, and timestamps each command from the engagement's
Atuin history. Full-screen TUIs (less, htop) render imperfectly; imported pasted logs
have best-effort timestamps.
