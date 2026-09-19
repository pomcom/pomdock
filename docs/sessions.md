# Sessions & reporting

Every shell opened via `pomdock docker exec` (or a Shells-tab window) is recorded with
`script` into `~/pentest/<engagement>/sessions/`, on a host volume so recordings survive
`docker rm`. Recording is armed by the `POMDOCK_ENGAGEMENT` variable the container's
zshrc checks, so it never fires on the host or in a plain `docker exec`. A shell opened
before the container picked up the hook is not recorded — open a fresh one.

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

- **Tracker** — the engagement's Atuin history as team-report rows, copyable as TSV.
- **Sessions** — recorded captures and imported logs, searchable across commands and
  output.

It reads the loot dir read-only and never touches the container image.

## How reading a recording works

A `script` log is a raw terminal byte stream: zsh's line editor draws the prompt and the
command you type with carriage returns, backspaces, and cursor-movement escapes,
redrawing the same line many times for syntax highlighting and autosuggestions. Opening a
recording runs this pipeline:

1. **Clean** — each physical line is replayed through a small cursor model that honours
   `\r`, `\b`, and the horizontal cursor escapes, so the command is reconstructed as it
   finally appeared on screen.
2. **Split** — the transcript is cut at `pomsession` markers into one logical session per
   tag.
3. **Parse** — each segment becomes command-and-output entries.
4. **Timestamp** — each command is matched against the engagement's Atuin history,
   floored at the session's start time so a repeated command like `ls` gets this
   session's time, in order. A command with no Atuin row in that window shows a blank
   time rather than a guessed one.

Full-screen TUIs (less, htop) render imperfectly, since each physical line is handled
independently. Imported pasted scrollback has no reliable start time, so its timestamps
stay best-effort.
