"""Turn a capture into timestamped, readable lines: what the terminal showed and when.

Line mode handles what shells and most tools write: text, colors, carriage returns,
backspaces and in-line cursor moves (prompts, progress bars, line editing). Each line
gets the time its newline was written, i.e. when it was complete: for a command line,
when Enter was pressed. Full-screen programs (vim, htop, less, tmux itself) switch to
the alternate screen; that span goes through a terminal emulator (pyte) and is kept as
a snapshot of the screen as it was when the program left it.

Transcripts are cached per capture in <session>/cache/, outside logs/ so the container
never sees them, and rebuilt when the capture or the transcriber (VERSION) changed.
"""

import codecs
import contextlib
import inspect
import fcntl
import os
import re
import threading
from collections import deque
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path

import pyte

from .capture import Capture

VERSION = 7  # Hide tmux window-name control strings in existing transcript caches.

# tmux/screen window rename, emitted by shell title hooks under TERM=tmux-*.
# This is terminal metadata, not command output (tmux(1), NAMES AND TITLES).
_WINDOW_NAME = re.compile(r"\x1bk[^\x1b]*\x1b\\")

_TOKEN = re.compile(
    r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)"  # OSC (window title etc.)
    r"|\x1bk[^\x1b]*\x1b\\"  # tmux/screen window rename
    r"|\x1b\[([0-9;?<=>]*)[ -/]*([@-~])"  # CSI: params, final byte
    r"|\x1b[()*+][0-9A-Za-z]"  # charset selection
    r"|\x1b[^\[\]()*+]"  # other two-byte escapes
    r"|[\x00-\x1f\x7f]"  # C0 controls, including \n \r \b \t and a lone ESC
)
_ALT = re.compile(r"\x1b\[\?([0-9;]*)([hl])")
_ALT_MODES = {"47", "1047", "1049"}
MAX_LINE = 1 << 20  # a line longer than this is cut, so a stream without newlines stays cheap
MAX_SCREEN = 2 << 20  # emulate at most this much of a full-screen span, the newest part
MAX_CARRY = 4096


@dataclass
class Line:
    t: float  # seconds since the capture started
    # o: a line of output; c: a command line, the line Enter was pressed on; S: a screen
    # snapshot starts; s: a line of it
    kind: str
    text: str
    record_id: str | None = None


class Emulator(pyte.Screen):
    """pyte's screen with the alternate screen of full-screen programs (pyte draws them
    over the normal screen, so it would still show vim after vim exits), and made to
    accept every escape sequence: pyte passes private=True for CSI sequences with a '?'
    (e.g. a device status query, ESC [ ? 6 n), but not all of its handlers take it."""

    def __init__(self, columns: int, lines: int):
        super().__init__(columns, lines)
        self.saved = None  # the normal screen while the alternate one is shown

    def set_mode(self, *modes, **kwargs):
        alt = kwargs.get("private") and _ALT_MODES.intersection(map(str, modes))
        if alt and self.saved is None:
            lines = {y: dict(line) for y, line in self.buffer.items()}
            self.saved = (lines, self.cursor.x, self.cursor.y)
            self.erase_in_display(2)
        super().set_mode(*modes, **kwargs)

    def reset_mode(self, *modes, **kwargs):
        alt = kwargs.get("private") and _ALT_MODES.intersection(map(str, modes))
        if alt and self.saved is not None:
            lines, x, y = self.saved
            self.saved = None
            self.buffer.clear()
            for row, line in lines.items():
                self.buffer[row].update(line)
            self.cursor.x, self.cursor.y = x, y
            self.dirty.update(range(self.lines))
        super().reset_mode(*modes, **kwargs)


def _drop_private(method):
    def handler(self, *args, private=False, **kwargs):
        return method(self, *args, **kwargs)

    return handler


for _name in set(pyte.Stream.csi.values()):
    _method = getattr(pyte.Screen, _name, None)
    if _method is not None:
        _params = inspect.signature(_method).parameters.values()
        if not any(p.name == "private" or p.kind is p.VAR_KEYWORD for p in _params):
            setattr(Emulator, _name, _drop_private(_method))


# zsh writes one terminal width of spaces and two carriage returns before each prompt
_PROMPT_SP = re.compile(r"( {10,})\r\r")
# cursor positioning, scroll regions and moves up or down: ESC[row;colH, ESC[rowd,
# ESC[top;bottomr, ESC[nA, ESC[nB
_POSITION = re.compile(r"\x1b\[(\d+)(?:;(\d+))?([HfdrAB])")


def infer_size(text: str, cols: int, rows: int) -> tuple[int, int]:
    """legacy: the terminal size as the output shows it. Captures don't always record resizes
    (script's classic timing format doesn't), so take the width from zsh's last prompt
    and grow both to the furthest position a program moved the cursor to. zsh redraws a
    command line taller than the screen by moving up nearly the whole screen, which shows
    the height in a plain shell too."""
    widths = _PROMPT_SP.findall(text)
    if widths:
        cols = len(widths[-1])
    for m in _POSITION.finditer(text):
        first, second, final = int(m.group(1)), m.group(2), m.group(3)
        if final == "r":
            rows = max(rows, int(second or 0))
        elif final == "d":
            rows = max(rows, first)
        elif final in "AB":
            if first < 250:  # not the "move as far as possible" idiom, e.g. ESC[999B
                rows = max(rows, first + 1)
        else:
            rows = max(rows, first)
            cols = max(cols, int(second or 0))
    return min(cols, 1000), min(rows, 500)


class _Screen:
    """A full-screen span, emulated when it ends. Only the newest MAX_SCREEN characters
    are kept, starting at a full clear when there is one."""

    def __init__(self, cols: int, rows: int):
        self.cols, self.rows = cols, rows
        self.parts: deque[tuple[float, str]] = deque()
        self.total = 0

    def add(self, t: float, text: str) -> None:
        if not text:
            return
        i = text.rfind("\x1b[2J")
        if i >= 0:
            self.parts.clear()
            self.total = 0
            text = text[i:]
        self.parts.append((t, text))
        self.total += len(text)
        while self.total > MAX_SCREEN and len(self.parts) > 1:
            self.total -= len(self.parts.popleft()[1])

    def snapshot(self, t: float) -> list[Line]:
        cols, rows = infer_size("".join(p for _, p in self.parts), self.cols, self.rows)
        screen = Emulator(cols, rows)
        stream = pyte.Stream(screen)
        when: dict[int, float] = {}
        for pt, text in self.parts:
            try:
                stream.feed(text)
            except Exception:
                break  # an emulator bug: keep the screen as far as it got
            for row in screen.dirty:
                when[row] = pt
            screen.dirty.clear()
        rows = [r.rstrip() for r in screen.display]
        while rows and not rows[-1]:
            rows.pop()
        if not any(rows):
            return []
        return [Line(t, "S", "")] + [Line(when.get(i, t), "s", r) for i, r in enumerate(rows)]


class Transcriber:
    def __init__(self, cols: int = 0, rows: int = 0):
        self.cols, self.rows = cols or 200, rows or 50
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.carry = ""
        # The current line, which may span several rows of the terminal: a command line
        # wider than the terminal wraps, and zsh redraws it row by row. `row` is where the
        # cursor's row starts in `line`, `col` the cursor's column in that row.
        self.line = ""
        self.row = 0
        self.col = 0
        self.screen: _Screen | None = None
        self.out: list[Line] = []
        self.t = 0.0
        self.command_stack = []
        self.entered = False  # the shell just took a command line

    def resize(self, cols: int, rows: int) -> None:
        self.cols, self.rows = cols, rows

    def feed(self, t: float, data: bytes) -> list[Line]:
        self.t = t
        text = self.carry + self.decoder.decode(data)
        self.carry = ""
        for token in _TOKEN.finditer(text):
            # Buffer from the beginning of an incomplete control string, not
            # its final ESC: a split string terminator may contain another ESC.
            if token[0] in ("\x1b", "\x1bk") and len(text) - token.start() < MAX_CARRY:
                self.carry, text = text[token.start():], text[:token.start()]
                break
        text = _WINDOW_NAME.sub("", text)
        if self.screen is None:  # legacy: a width change zsh shows, see infer_size
            widths = _PROMPT_SP.findall(text)
            if widths:
                self.cols = len(widths[-1])
        pos = 0
        for m in _ALT.finditer(text):
            if not _ALT_MODES.intersection(m.group(1).split(";")):
                continue
            self._segment(text[pos : m.start()])
            pos = m.end()
            if m.group(2) == "h" and self.screen is None:
                self._newline_if_pending()
                self.screen = _Screen(self.cols, self.rows)
            elif m.group(2) == "l" and self.screen is not None:
                self._snapshot(t)
                self.screen = None
        self._segment(text[pos:])
        out, self.out = self.out, []
        return out

    def finish(self) -> list[Line]:
        if self.carry:
            self._segment(self.carry)
            self.carry = ""
        if self.screen is not None:
            self._snapshot(self.t)
            self.screen = None
        self._newline_if_pending()
        out, self.out = self.out, []
        return out

    def _newline_if_pending(self) -> None:
        if self.line.strip():
            self._emit()
        self.line, self.row, self.col = "", 0, 0

    def _snapshot(self, t):
        snapshot = self.screen.snapshot(t)
        ident = self.command_stack[-1] if self.command_stack else None
        for ln in snapshot:
            ln.record_id = ident
        self.out.extend(snapshot)

    def _emit(self) -> None:
        self.out.append(Line(self.t, "c" if self.entered else "o", self.line.rstrip(), self.command_stack[-1] if self.command_stack else None))
        self.entered = False

    def _segment(self, text: str) -> None:
        if not text:
            return
        if self.screen is not None:
            self.screen.add(self.t, text)
            return
        pos = 0
        for m in _TOKEN.finditer(text):
            if m.start() > pos:
                self._write(text[pos : m.start()])
            pos = m.end()
            tok = m.group(0)
            boundary = re.match(r"\x1b\]777;pomdock-(start|end);([^\x07\x1b]+)", tok)
            if boundary:
                self._newline_if_pending()
                ident = boundary[2]
                if boundary[1] == "start":
                    self.command_stack.append(ident)
                elif ident in self.command_stack:
                    self.command_stack = self.command_stack[:self.command_stack.index(ident)]
            elif tok == "\n":
                self._emit()
                self.line, self.row, self.col = "", 0, 0
            elif tok == "\r":
                self.col = 0
            elif tok == "\b":
                self.col = max(0, self.col - 1)
            elif tok == "\t":
                self._move((self.col // 8 + 1) * 8)
            elif m.group(2):
                self._csi(m.group(1), m.group(2))
        if pos < len(text):
            self._write(text[pos:])

    def _move(self, col: int) -> None:
        """To a column of the current row; the terminal stops at its last column."""
        self.col = min(max(0, col), self.cols - 1)

    def _put(self, s: str) -> None:
        line, i = self.line, self.row + self.col
        if i >= len(line):
            self.line = line + " " * (i - len(line)) + s
        else:
            self.line = line[:i] + s + line[i + len(s) :]
        self.col += len(s)

    def _write(self, s: str) -> None:
        cols = self.cols
        while s:
            if self.col >= cols:  # past the last column: the terminal wraps to a new row
                self.row += cols
                self.col = 0
            n = min(len(s), cols - self.col)
            self._put(s[:n])
            s = s[n:]
        if len(self.line) > MAX_LINE:
            self._emit()
            self.line, self.row, self.col = "", 0, 0

    def _csi(self, params: str, final: str) -> None:
        if params == "?2004" and final == "l":
            # The shell turns bracketed paste off when Enter is pressed: zsh before the
            # newline (the line ending next is the command line), bash after it (the
            # line that just ended is).
            if not self.line.strip() and self.out and self.out[-1].kind == "o":
                self.out[-1] = Line(self.out[-1].t, "c", self.out[-1].text)
            else:
                self.entered = True
        if params.startswith(("?", "<", "=", ">")):
            return  # private modes: cursor visibility, bracketed paste, ...
        nums = [int(p) if p.isdigit() else 0 for p in params.split(";")] if params else []
        n = nums[0] if nums else 0
        k = max(n, 1)
        start, end, i = self.row, self.row + self.cols, self.row + self.col
        line = self.line
        if final == "K":  # erase in the cursor's row
            lo, hi = (i, end) if n == 0 else (start, i + 1) if n == 1 else (start, end)
            if hi >= len(line):
                self.line = line[:lo]
            else:
                self.line = line[:lo] + " " * (hi - lo) + line[hi:]
        elif final == "J" and n == 0:  # erase from the cursor to the end of the screen
            self.line = line[:i]
        elif final == "A":  # up: an earlier row of this line, as far as it goes
            self.row = max(0, self.row - k * self.cols)
        elif final == "B":
            self.row += k * self.cols
        elif final == "C":
            self._move(self.col + k)
        elif final == "D":
            self.col = max(0, min(self.col, self.cols - 1) - k)
        elif final == "G":
            self._move(k - 1)
        elif final in "Hf":
            self._move((nums[1] if len(nums) > 1 and nums[1] else 1) - 1)
        elif final in "P@X":  # delete, insert or erase characters within the row
            row = line[start:end]
            c = self.col
            if final == "P":
                row = row[:c] + row[c + k :]
            elif final == "@":
                row = (row[:c] + " " * k + row[c:])[: self.cols]
            else:
                row = (row[:c] + " " * k + row[c + k :])[: max(len(row), c)]
            if end >= len(line):
                self.line = line[:start] + row
            else:
                self.line = line[:start] + row.ljust(self.cols) + line[end:]


def transcribe(cap: Capture) -> Iterator[Line]:
    """The whole transcript, uncached."""
    tr = Transcriber(*cap.size())
    for chunk in cap.chunks():
        if chunk.kind == "r":
            tr.resize(*(int(x) for x in chunk.data.split(b"x")))
        else:
            yield from tr.feed(chunk.t, chunk.data)
    yield from tr.finish()


# --- cache --------------------------------------------------------------------------


def _escape(s: str) -> str:
    return s.replace("\\", "\\\\").replace("\t", "\\t").replace("\n", "\\n")


def _unescape(s: str) -> str:
    if "\\" not in s:
        return s
    return re.sub(r"\\(.)", lambda m: {"t": "\t", "n": "\n"}.get(m.group(1), m.group(1)), s)


def cache_path(cache_dir: Path, cap: Capture) -> Path:
    return cache_dir / f"{cap.sub}.tsv"


def _command_lines_path(cache_dir: Path, cap: Capture) -> Path:
    return cache_dir / f"{cap.sub}.commands.tsv"


_locks: dict[Path, threading.Lock] = {}
_locks_lock = threading.Lock()


@contextlib.contextmanager
def building(path: Path):
    """One thread at a time builds a cache file (kalico show serves requests in parallel);
    the others wait for it and then find it current."""
    with _locks_lock:
        lock = _locks.setdefault(path, threading.Lock())
    path.parent.mkdir(parents=True, exist_ok=True)
    with lock, open(path.with_suffix(path.suffix + ".lock"), "a") as guard:
        fcntl.flock(guard.fileno(), fcntl.LOCK_EX)
        yield


def scratch(path: Path, tag: str) -> Path:
    """A temporary file next to `path`, unique to this process and thread."""
    return path.with_suffix(f".{tag}{os.getpid()}-{threading.get_ident()}")


def update_cache(cache_dir: Path, cap: Capture) -> Path:
    """Rebuild the cached transcript when the capture changed since it was made. The
    first line of the cache records what it was made from."""
    tsv = cache_path(cache_dir, cap)
    stamp = f"# pomdock transcript v{VERSION} {cap.kind} {cap.signature()} {cap.finished()}\n"

    def current() -> bool:
        try:
            with open(tsv) as f:
                return f.readline() == stamp
        except OSError:
            return False

    if current():
        return tsv
    with building(tsv):
        if not current():  # another thread may have built it meanwhile
            _build_cache(cache_dir, cap, tsv, stamp)
    return tsv


def _build_cache(cache_dir: Path, cap: Capture, tsv: Path, stamp: str) -> None:
    cache_dir.mkdir(parents=True, exist_ok=True)
    tmp, ctmp = scratch(tsv, "tmp"), scratch(tsv, "ctmp")
    try:
        with open(tmp, "w", encoding="utf-8") as f, open(ctmp, "w", encoding="utf-8") as c:
            f.write(stamp)
            for ln in transcribe(cap):
                row = f"{ln.t:.3f}\t{ln.kind}\t{ln.record_id or ''}\t{_escape(ln.text)}\n"
                f.write(row)
                if ln.kind == "c":
                    c.write(row)
        # atomic, so a concurrent search never reads half a cache; the transcript last,
        # since its stamp says both are current
        ctmp.replace(_command_lines_path(cache_dir, cap))
        tmp.replace(tsv)
    finally:
        tmp.unlink(missing_ok=True)
        ctmp.unlink(missing_ok=True)


def _rows(path: Path, skip: int = 0) -> Iterator[Line]:
    """The lines of a cache file; a damaged row is skipped rather than failing."""
    with open(path, encoding="utf-8", errors="replace", newline="\n") as f:
        for _ in range(skip):
            f.readline()
        for row in f:
            try:
                t, kind, ident, text = row.rstrip("\n").split("\t", 3)
                yield Line(float(t), kind, _unescape(text), ident or None)
            except ValueError:
                continue


def command_lines(cache_dir: Path, cap: Capture) -> list[Line]:
    """The lines Enter was pressed on, through the cache. legacy: only timeline.anchors
    reads them."""
    update_cache(cache_dir, cap)
    return list(_rows(_command_lines_path(cache_dir, cap)))


def lines(cache_dir: Path, cap: Capture) -> Iterator[Line]:
    """The capture's transcript, through the cache."""
    yield from _rows(update_cache(cache_dir, cap), skip=1)
