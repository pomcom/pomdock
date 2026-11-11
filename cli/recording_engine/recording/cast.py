"""A capture as an asciicast v2 recording, for the asciinema web player.

Pauses longer than IDLE seconds are shortened to IDLE when the cast is written, so the
player's time is the cast's time. `breaks` records where that happened: (cast time,
capture time) pairs, from which cast time, capture time and, through the timeline clock,
real time map onto each other. Resizes the capture recorded become resize events; legacy:
for older captures, so do width changes zsh shows at its prompts.

Casts are cached next to the transcripts and rebuilt when the capture changes.
"""

import bisect
import codecs
import json
import re
from pathlib import Path

from . import commands, timefmt, timeline, transcript
from .capture import Capture
from .session import Session

VERSION = 2  # 2: casts built before parallel builds were serialized may be damaged
IDLE = 2.0
AFTER = 0.001  # how far after a moment to show it, so writes at that moment are drawn
_PROMPT_SP = re.compile(r"( {10,})\r\r")  # zsh: one terminal width of spaces per prompt


def _paths(s: Session, cap: Capture) -> tuple[Path, Path]:
    return s.cache / f"{cap.sub}.cast", s.cache / f"{cap.sub}.cast.json"


def build(s: Session, cap: Capture) -> tuple[Path, list[tuple[float, float]]]:
    """The cached cast, rebuilt if the capture changed, and its breaks."""
    path, meta_path = _paths(s, cap)
    stamp = f"v{VERSION} {cap.kind} {cap.signature()} {cap.finished()}"

    def cached():
        try:
            meta = json.loads(meta_path.read_text())
            if meta["stamp"] == stamp and path.is_file():
                return [tuple(b) for b in meta["breaks"]]
        except (OSError, ValueError, KeyError):
            pass
        return None

    breaks = cached()
    if breaks is not None:
        return path, breaks
    with transcript.building(path):
        breaks = cached()  # another thread may have built it meanwhile
        if breaks is not None:
            return path, breaks
        s.cache.mkdir(parents=True, exist_ok=True)
        tmp = transcript.scratch(path, "tmp")
        try:
            with open(tmp, "w", encoding="utf-8") as f:
                breaks = _write(cap, f)
            tmp.replace(path)
        finally:
            tmp.unlink(missing_ok=True)
        meta_tmp = transcript.scratch(meta_path, "tmp")
        meta_tmp.write_text(json.dumps({"stamp": stamp, "breaks": breaks}))
        meta_tmp.replace(meta_path)
    return path, breaks


def _write(cap: Capture, f) -> list[tuple[float, float]]:
    cols, rows = cap.size()
    cols, rows = cols or 80, rows or 24
    start = cap.start()
    header = {"version": 2, "width": cols, "height": rows}
    if start:
        header["timestamp"] = int(start.timestamp())
    f.write(json.dumps(header) + "\n")
    decoder = codecs.getincrementaldecoder("utf-8")("replace")
    breaks: list[tuple[float, float]] = [(0.0, 0.0)]
    ct, last = 0.0, 0.0
    for chunk in cap.chunks():
        gap = chunk.t - last
        if gap > IDLE:
            ct += IDLE
            breaks.append((round(ct, 6), chunk.t))
        else:
            ct += max(gap, 0.0)
        last = chunk.t
        if chunk.kind == "r":
            cols, rows = (int(x) for x in chunk.data.split(b"x"))
            f.write(json.dumps([round(ct, 6), "r", f"{cols}x{rows}"]) + "\n")
            continue
        text = decoder.decode(chunk.data)
        widths = _PROMPT_SP.findall(text)
        if widths and len(widths[-1]) != cols:  # legacy: a resize only zsh shows
            cols = len(widths[-1])
            f.write(json.dumps([round(ct, 6), "r", f"{cols}x{rows}"]) + "\n")
        if text:
            f.write(json.dumps([round(ct, 6), "o", text]) + "\n")
    return breaks


def cast_time(breaks: list[tuple[float, float]], t: float) -> float:
    """Cast time of capture time t; inside a shortened pause, its end."""
    i = bisect.bisect_right([b[1] for b in breaks], t) - 1
    if i < 0:
        return 0.0
    ct = breaks[i][0] + (t - breaks[i][1])
    return min(ct, breaks[i + 1][0]) if i + 1 < len(breaks) else ct


def capture_time(breaks: list[tuple[float, float]], ct: float) -> float:
    i = bisect.bisect_right([b[0] for b in breaks], ct) - 1
    return breaks[max(i, 0)][1] + (ct - breaks[max(i, 0)][0])


def info(s: Session, cap: Capture) -> dict:
    """What the page needs to play a shell: its size, markers for its commands and a
    table from cast time to real time ([cast time, epoch ms] points; between points,
    both advance together)."""
    _, breaks = build(s, cap)
    clock = timeline.clock(s.cache, s.logdir, cap)
    points = [(ct, clock.real(t)) for ct, t in breaks]
    points += [(cast_time(breaks, t), real) for t, real in clock.anchors]
    table = sorted({(round(ct, 3), round(real * 1000)) for ct, real in points})
    markers = []
    for c in commands.read(s.logdir):
        if c.subid != cap.sub:
            continue
        # just after Enter: the command line is on screen, its output not yet
        ct = cast_time(breaks, clock.capture(c.start.timestamp())) + AFTER
        label = f"{timefmt.show(c.start)[11:19]} $ {' '.join(c.cmd.split())[:120]}"
        markers.append([round(ct, 3), label])
    cols, rows = cap.size()
    return {"sub": cap.sub, "width": cols or 80, "height": rows or 24, "table": table,
            "markers": sorted(markers)}  # fmt: skip
