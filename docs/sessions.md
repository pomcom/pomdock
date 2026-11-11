# Recordings & Tracker

`pomdock docker exec NAME` and managed container terminals record interactive
zsh/bash shells into the workspace's `sessions/` directory. Atuin remains enabled.
The image-owned wrapper handles recording independently of personal dotfiles;
plain `docker exec` does not enable it.

## Find and read commands

```bash
pomdock sessions list                           # available engagements
pomdock sessions list acme                      # recordings
pomdock sessions logs acme                      # shell IDs
pomdock sessions logs acme SHELL --clean        # readable complete transcript
pomdock sessions search 'open' acme             # literal, case-sensitive search
pomdock sessions search 'timeout|denied' --all --regex -i
pomdock sessions show acme 'open'               # open timeline with search
pomdock sessions import scrollback.txt --name acme
```

`logs NAME SHELL` without `--clean` prints the original terminal stream, including
escape sequences. Search includes recorded commands, output and pasted imports.
GUI edits and deletions apply by default; `--original` searches unedited content,
including hidden items. `--archived` selects individual archived recordings;
encrypted engagement packages must be restored before searching.

Inside a recorded shell, `pdsession recon` (or `pomsession start recon`) starts a
labelled section without interrupting recording. `help` shows recording state,
networking commands and RDP helpers.

## Work in the GUI

`pomdock web` opens the local workspace. It binds only to 127.0.0.1; the printed URL
logs the browser in. `--token-file FILE` preserves login across restarts.

- **Session history**: search the engagement-wide timeline, select a command or hit,
  and replay its original shell at that moment. Literal/regex and case controls
  apply to the selected live or archived scope. Full-screen search uses terminal
  snapshots; replay retains the original stream.
- **Recordings & edits**: rename recordings, edit command/output text, and hide noise.
  Select entries, Shift-click a range, or select matching entries across pages for
  bulk deletion. **Show deleted** restores hidden items; **Restore original** resets
  edits. Copy output includes its complete contents, even when previewed collapsed.
- **Tracker**: edit engagement history for copying into an external tracker.
  Row selection controls bulk actions; Export checkboxes independently control TSV
  inclusion. Completed indexed commands absent from Atuin are included when known.
- **Containers / Terminals**: manage containers and enter their per-container tmux
  sessions. VM terminals and jobs use the shared workspace.

Names, edits and deletions live under `<loot>/.pomdock/`; originals and Atuin are
preserved. Replay always shows the original stream, which can differ from edited
text. Missing timestamps, exit codes and command boundaries remain unknown.
An open capture means no end marker was recorded; it does not prove a live shell.

## Archive and restore

**Archive & encrypt** bundles recordings, indexes, Atuin, Tracker and GUI edits into
one `<loot>/archives/*.tar.age` file. Stop engagement containers first. The verified
package replaces live history; unrelated loot stays in place. Archives remain
visible in the GUI and can be restored with the passphrase. CLI restoration:

```bash
pomdock sessions restore /path/to/acme.tar.age
```

Restoring never overwrites existing live history. Individual recording archives keep
their `.log`, `.timing`, `.start` and command-index sidecars together.

## Runtime

Current images write timed terminal captures and a version-2 structured command
index. To upgrade older containers, rebuild the image and recreate containers while
preserving loot. Legacy recordings remain available in **Recordings & edits**;
exact metadata cannot be added retroactively.

The host needs Python 3.11+. Processing modules and replay assets are bundled;
there are no runtime downloads. Disposable derived caches live in
`.pomdock/cache/recordings/`.

Workspace data defaults to `~/pomdock/workspaces/NAME` on the host and `~/workspace`
in the container. `~/pentest` remains a compatibility alias; recorded paths are
kept as originally captured.
