"""Map between a capture's own time and the real time.

A capture's time is the sum of the pauses script(1) recorded between writes. While the
host sleeps, the Docker VM is frozen and that time is not recorded, so after each sleep
the capture's time falls behind the real time. The command index has the real time of
every command; newer images also record where in the capture each command starts and
ends, which gives exact anchors. legacy: for older captures, the transcript's command lines
(where Enter was pressed) are matched with the commands in order. Between anchors time
runs as recorded.
"""

import bisect
from dataclasses import dataclass
from pathlib import Path

from . import commands, transcript
from .capture import Capture, boundaries

LOOKAHEAD = 50  # commands to look ahead for a command line's match (skipping unmatched)
PROBE = 24  # characters of a command's start and end to look for in its command line
SLACK = 2.0  # seconds a match may run ahead of the drift so far (start-time precision)


@dataclass
class Clock:
    begin: float  # real start of the capture, epoch seconds
    anchors: list[tuple[float, float]]  # (capture time, real epoch time), both ascending

    def real(self, t: float) -> float:
        """Epoch seconds of capture time t."""
        i = bisect.bisect_right([a[0] for a in self.anchors], t) - 1
        if i < 0:
            return self.begin + t
        at, real = self.anchors[i]
        return real + (t - at)

    def capture(self, real: float) -> float:
        """Capture time of a real time. A time while nothing was recorded (the host
        slept) maps to the end of what was recorded before it."""
        reals = [a[1] for a in self.anchors]
        i = bisect.bisect_right(reals, real) - 1
        if i < 0:
            t = real - self.begin
            upper = self.anchors[0][0] if self.anchors else None
        else:
            at, areal = self.anchors[i]
            t = at + (real - areal)
            upper = self.anchors[i + 1][0] if i + 1 < len(self.anchors) else None
        if upper is not None:
            t = min(t, upper - 0.001)
        return max(0.0, t)


def _probes(cmd: str) -> list[str]:
    """Pieces of a command to look for in its command line: its start and its end, with
    spaces collapsed. A long line that zsh redrew over several rows may come out
    garbled in places; one intact piece is enough."""
    first = " ".join(cmd.split("\n", 1)[0].split())
    if len(first) <= PROBE:
        return [first] if first else []
    return [first[:PROBE], first[-PROBE:]]


def anchors(begin: float, lines: list[transcript.Line], cmds: list[commands.Command]):
    """legacy: pair command lines with commands, in order. A command line pairs with one of the
    next LOOKAHEAD commands that could have run then (the capture can fall behind the
    real time, never get ahead of it: the drift doesn't shrink) and whose start or end
    it contains. Empty Enters and commands without a recognizable line are skipped."""
    out: list[tuple[float, float]] = []
    starts = [c.start.timestamp() for c in cmds]
    probes = [_probes(c.cmd) for c in cmds]
    j, drift = 0, 0.0
    for ln in lines:
        text = " ".join(ln.text.split())
        first = max(j, bisect.bisect_left(starts, begin + ln.t + drift - SLACK))
        for k in range(first, min(first + LOOKAHEAD, len(cmds))):
            if any(p in text for p in probes[k]):
                out.append((ln.t, starts[k]))
                drift = max(drift, starts[k] - (begin + ln.t))
                j = k + 1
                break
    return out


def exact_anchors(cap: Capture, cmds: list[commands.Command]) -> list[tuple[float, float]]:
    """Anchors from where in the capture newer images record each command to start and
    end (pos, end_pos): exact, whatever the capture's own clock did."""
    _, spans = boundaries(cap)
    for c in cmds:
        if c.record_id in spans:
            c.pos, c.end_pos = spans[c.record_id]
    points = sorted(
        (pos, when.timestamp())
        for c in cmds
        for pos, when in ((c.pos, c.start), (c.end_pos, c.end))
        if pos is not None and when is not None
    )
    times = cap.times_at([p for p, _ in points])
    return [(t, real) for t, (_, real) in zip(times, points, strict=True) if t is not None]


def _monotonic(points: list[tuple[float, float]]) -> list[tuple[float, float]]:
    """Anchors ascending in both capture and real time; ones that contradict that go."""
    out: list[tuple[float, float]] = []
    for t, real in sorted(points):
        if not out or (t >= out[-1][0] and real >= out[-1][1]):
            out.append((t, real))
    return out


def clock(cache_dir: Path, logdir: Path, cap: Capture) -> Clock:
    start = cap.start()
    begin = start.timestamp() if start else 0.0
    cmds = [c for c in commands.read(logdir) if c.subid == cap.sub]
    if not cmds or not start:
        return Clock(begin, [])
    exact = exact_anchors(cap, cmds)
    rest = [c for c in cmds if c.pos is None]  # legacy
    matched = anchors(begin, transcript.command_lines(cache_dir, cap), rest) if rest else []
    return Clock(begin, _monotonic(exact + matched))
