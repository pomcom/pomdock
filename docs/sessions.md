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
- **Session history**: recorded captures and imported logs, searchable across commands and
  output.
- **Containers**: create, start, stop, recreate, and remove pentest containers.
- **Terminals**: existing shell windows in the pomdock tmux workspace. Open a new
  container terminal from Containers; its recording appears in Session history.

The navigation and recording list collapse independently. Drag the list divider
(or focus it and use arrow keys, Home, or End) to resize it between 260–300px.
Layout settings and session display names use local pomdock preferences. Rename
changes the display name only; the recording ID and captured history stay intact.

Filter recordings by name, date, or tag. Search explicitly targets the current
session or all non-archived recordings in the selected engagement. Enter and
Shift+Enter navigate matches. Histories render 100 executions per page; search
jumps to the page containing its match. Collapsed output previews never truncate
what the copy button copies. Recording IDs, archive, restore, and encryption
actions are available under Details; archive/encrypt affects the entire capture,
including its tagged sections.

**Open capture** means no end marker was recorded, including captures interrupted
without a clean shutdown. It does not prove the terminal is live. Open captures
refresh every three seconds while the page is visible. Scrolling back pauses
following; **Follow output** resumes at the latest execution. Completed segments
and captures show **Ended**. Container identity, command exit status, and duration
are unavailable in the existing transcript format and are not inferred.

## How reading a recording works

A raw `script` log is full of the line editor's redraw noise. Opening a recording
reconstructs each typed command (replaying `\r`, `\b`, and cursor moves), splits the
transcript at `pomsession` markers, and timestamps each command from the engagement's
Atuin history. Full-screen TUIs (less, htop) render imperfectly; imported pasted logs
have best-effort timestamps.
