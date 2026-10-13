# Sessions & reporting

Every shell opened via `pomdock docker exec` (or a Shells-tab window) is recorded with
`script` into `~/pentest/<engagement>/sessions/`, on a host volume so recordings survive
`docker rm`. Recording is armed by `POMDOCK_ENGAGEMENT` and handled by the image-owned
`pomdock-shell` wrapper, so it does not depend on personal dotfiles and never fires on
the host or in a plain `docker exec`. An image built before this wrapper was added must
be rebuilt once with `pomdock docker build`.

## Tagging sub-sessions

Inside a recorded shell, split a long capture into labelled sessions:

```
pomsession start recon      # begin a session tagged "recon"
pomsession start exploit    # everything after this becomes a new "exploit" session
pdsession recon             # short alias for the same thing
pomsession help             # (or pomhelp) list the helpers and show recording state
```

Each `pomsession start` writes a marker the Sessions tab splits on, so one capture can
hold several tagged sessions you filter by in the UI. It warns instead of tagging if the
shell is not being recorded.

## Browsing and exporting

```bash
pomdock web                                      # aliases: tracker, report
pomdock web --name acme                          # open an engagement directly
pomdock web import scrollback.txt --name acme    # import a pasted log
```

Binds 127.0.0.1 only; the printed URL carries a per-run login token. `--token-file FILE`
keeps one token across restarts (created 0600 if missing). Tabs:

- **Tracker**: the engagement's Atuin history as team-report rows, copyable as TSV.
  Curation is saved to `<loot>/.pomdock/tracker.json`.
- **Sessions**: recorded captures and imported logs, searchable across commands and
  output.
- **Containers**: create, start, stop, recreate, and remove pentest containers.
- **Shells**: open recorded container shells in the pomdock tmux workspace.

## How reading a recording works

A raw `script` log is full of the line editor's redraw noise. Opening a recording
reconstructs each typed command (replaying `\r`, `\b`, and cursor moves), splits the
transcript at `pomsession` markers, and timestamps each command from the engagement's
Atuin history. Full-screen TUIs (less, htop) render imperfectly; imported pasted logs
have best-effort timestamps.
