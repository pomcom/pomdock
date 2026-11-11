"""Captures of a session's shells, made by util-linux script(1): <subid>.log (output),
<subid>.timing and <subid>.start (the start to the millisecond).

A shell's id (subid) is <UTC time>-<context>-pid<pid>, e.g. 20261012T165140Z-main-pid45; the
context is main, bg (the container's background shell) or tmux-<session>_<win>_<pane>.

The .log starts with a "Script started on ..." header line and, after a normal exit,
ends with a "Script done on ..." trailer; the timing file covers only what is between.
It is in script's advanced format ("O|I <delay> <bytes>", "H <delay> <name> <value>",
"S <delay> SIGWINCH ROWS=<r> COLS=<c>"); delays are seconds since the previous entry.
legacy: older images wrote the classic format ("<delay> <bytes>" per output write).
"""

import re
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from . import timefmt
from .config import Die

_HEADER = b"Script started on "
_TRAILER = b"Script done on "


@dataclass
class Chunk:
    """One write to the terminal; t is seconds since the capture started."""

    t: float
    data: bytes
    kind: str = "o"  # o: output, r: resize (data is b"COLSxROWS")


class Capture:
    """A recorded shell of a session."""

    kind = "script"  # part of the cache stamps

    def __init__(self, logdir: Path, sub: str):
        self.logdir = logdir
        self.sub = sub
        self._header: bytes | None = None

    @property
    def main_file(self) -> Path:
        return self.logdir / f"{self.sub}.log"

    @property
    def timing_file(self) -> Path:
        return self.logdir / f"{self.sub}.timing"

    def header(self) -> bytes:
        if self._header is None:
            try:
                with open(self.main_file, "rb") as f:
                    first = f.readline(65536)
            except OSError:
                first = b""
            self._header = first if first.startswith(_HEADER) and first.endswith(b"\n") else b""
        return self._header

    def start(self) -> datetime | None:
        """Absolute start time, from <subid>.start."""
        try:
            precise = timefmt.parse((self.logdir / f"{self.sub}.start").read_text().strip())
        except OSError:
            precise = None
        return precise or self.recorded_start()

    def precise_start(self) -> bool:
        return (self.logdir / f"{self.sub}.start").is_file()

    def recorded_start(self) -> datetime | None:
        """legacy: script's own start time, to the second, for captures without .start."""
        m = re.match(rb"Script started on (\S+ \S+)", self.header())
        if m:
            try:
                return datetime.strptime(m.group(1).decode(), "%Y-%m-%d %H:%M:%S%z")
            except ValueError:
                pass
        return None

    def size(self) -> tuple[int, int]:
        """(columns, rows) of the terminal when the capture started, if recorded."""
        h = self.header()
        cols = re.search(rb'COLUMNS="(\d+)"', h)
        rows = re.search(rb'LINES="(\d+)"', h)
        return (int(cols.group(1)) if cols else 0, int(rows.group(1)) if rows else 0)

    def finished(self) -> bool:
        """True when the shell exited normally (script wrote its trailer)."""
        try:
            with open(self.main_file, "rb") as f:
                f.seek(0, 2)
                f.seek(max(0, f.tell() - 512))
                tail = f.read()
        except OSError:
            return False
        last = tail.rstrip(b"\n").rsplit(b"\n", 1)[-1]
        return last.startswith(_TRAILER)

    def chunks(self) -> Iterator[Chunk]:
        """Output writes and resizes in order; stops at an entry that is still being
        written."""
        if not (self.timing_file.is_file() and self.main_file.is_file()):
            return
        with open(self.timing_file, "rb") as tf, open(self.main_file, "rb") as lf:
            lf.seek(len(self.header()))
            t = 0.0
            while True:
                line = tf.readline()
                if not line.endswith(b"\n"):
                    return
                parts = line.split()
                if not parts:
                    continue
                if parts[0][:1].isdigit():  # legacy: classic format
                    kind, delay, rest = b"O", parts[0], parts[1:]
                else:
                    kind, delay, rest = parts[0], parts[1] if len(parts) > 1 else b"0", parts[2:]
                try:
                    t += float(delay)
                except ValueError:
                    continue
                if kind == b"O" and rest:
                    n = int(rest[0])
                    data = lf.read(n)
                    if len(data) < n:
                        return  # timing ran ahead of the flushed output
                    yield Chunk(t, data)
                elif kind == b"S" and b"SIGWINCH" in rest:
                    m = re.search(rb"ROWS=(\d+).*COLS=(\d+)", b" ".join(rest))
                    if m:
                        size = m.group(2) + b"x" + m.group(1)
                        yield Chunk(t, size, kind="r")

    def times_at(self, offsets: list[int]) -> list[float | None]:
        """Capture time at which the .log file (header included) reached each byte
        offset (ascending), or None if it hasn't; one pass over the timing file."""
        out: list[float | None] = [None] * len(offsets)
        if not offsets or not self.timing_file.is_file():
            return out
        size, t, i = len(self.header()), 0.0, 0
        while i < len(offsets) and offsets[i] <= size:
            out[i], i = 0.0, i + 1
        with open(self.timing_file, "rb") as tf:
            for line in tf:
                parts = line.split()
                if len(parts) < 2:
                    continue
                # legacy: a leading digit is the classic format
                kind, rest = (b"O", parts) if parts[0][:1].isdigit() else (parts[0], parts[1:])
                try:
                    t += float(rest[0])
                except ValueError:
                    continue
                if kind == b"O" and len(rest) > 1:
                    size += int(rest[1])
                    while i < len(offsets) and offsets[i] <= size:
                        out[i], i = t, i + 1
                    if i == len(offsets):
                        break
        return out

    def signature(self):
        parts = []
        for path in (self.main_file, self.timing_file, self.logdir / f"{self.sub}.start"):
            try:
                st = path.stat()
                parts.append(f"{st.st_size}:{st.st_mtime_ns}")
            except OSError:
                parts.append("missing")
        return " ".join(parts)

    def nbytes(self) -> int:
        try:
            return self.main_file.stat().st_size
        except OSError:
            return 0


# Invisible markers delimit actual output even when script's file-size writes
# lag behind shell hooks. Raw source remains untouched and replay consumes OSC.
_BOUNDARY = re.compile(rb"\x1b\]777;pomdock-(start|end);([^\x07\x1b]+)(?:\x07|\x1b\\)")

def boundaries(cap):
    if hasattr(cap, '_boundary_result'):
        return cap._boundary_result
    raw = cap.main_file.read_bytes()
    spans = {}
    for m in _BOUNDARY.finditer(raw):
        ident = m[2].decode("utf-8", "replace")
        if m[1] == b"start":
            spans.setdefault(ident, [m.end(), None])
        elif ident in spans and spans[ident][1] is None:
            spans[ident][1] = m.start()
    cap._boundary_result = (raw, spans)
    return raw, spans


def _order(sub: str) -> tuple[str, int]:
    m = re.search(r"-pid(\d+)$", sub)
    return (sub.split("-", 1)[0], int(m.group(1)) if m else 0)


def subids(logdir: Path) -> list[str]:
    """Shell ids, oldest first: by their UTC timestamp prefix, then by pid."""
    return sorted((p.name[: -len(".timing")] for p in logdir.glob("*.timing")), key=_order)


def is_background(sub: str) -> bool:
    return "-bg-pid" in sub


def open_capture(logdir: Path, sub: str) -> Capture | None:
    return Capture(logdir, sub) if (logdir / f"{sub}.timing").is_file() else None


def pick(logdir: Path, session: str, sub: str | None) -> Capture:
    """The capture SUB, or by default the newest shell someone used (the background
    shell only if there is nothing else)."""
    if not sub:
        subs = subids(logdir)
        used = [s for s in subs if not is_background(s)]
        if not subs:
            raise Die(f"session {session} has no captured shells yet")
        sub = (used or subs)[-1]
    cap = open_capture(logdir, sub)
    if cap is None:
        raise Die(f"no shell '{sub}' in session {session}")
    return cap
