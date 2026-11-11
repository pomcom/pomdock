"""Search what the terminals showed, and when.

Hits come from the timestamped transcripts of each capture; each hit is shown under the
command that was running at that time (from the command index of the same shell).
"""

import bisect
import re
from collections.abc import Iterable
from dataclasses import dataclass, field
from datetime import UTC, datetime

from . import capture, commands, timefmt, timeline, transcript
from .commands import Command
from .session import Session


@dataclass
class Hit:
    t: datetime
    text: str
    kind: str  # o: output line, c: a command line as echoed, s: a full-screen program's


@dataclass
class Group:
    session: str
    subid: str
    command: Command | None
    hits: list[Hit] = field(default_factory=list)


def search_capture(
    s: Session, cap: capture.Capture, cmds: list[Command], pattern: re.Pattern
) -> list[Group]:
    if cap.start() is None:
        return []
    clock = timeline.clock(s.cache, s.logdir, cap)
    starts = [c.start.timestamp() for c in cmds]
    # A command line is written a moment before its hook records the start. Captures
    # without a .start file (legacy) only know their start to the second, so allow for that.
    slack = 0.25 if cap.precise_start() else 1.0
    groups: list[Group] = []
    by_id = {c.record_id:c for c in cmds if c.record_id}
    _, marked = capture.boundaries(cap)
    for ln in transcript.lines(s.cache, cap):
        if ln.kind == "S" or not pattern.search(ln.text):
            continue
        t = clock.real(ln.t)
        i = bisect.bisect_right(starts, t) - 1
        ahead = bisect.bisect_right(starts, t + slack) - 1
        # Echoed input can precede preexec slightly. Do not apply that allowance
        # to arbitrary output or it can be assigned to the following command.
        if ahead > i and (ln.kind == "c" or cmds[ahead].cmd.strip() in ln.text):
            i = ahead
        cmd = cmds[i] if i >= 0 else None
        if marked:
            cmd = by_id.get(ln.record_id)
        elif cmd and cmd.end and t > cmd.end.timestamp() + slack:
            cmd = None  # between commands: the prompt
        if not groups or groups[-1].command is not cmd:
            groups.append(Group(s.name, cap.sub, cmd))
        groups[-1].hits.append(Hit(datetime.fromtimestamp(t, UTC), ln.text, ln.kind))
    return groups


def search_session(s: Session, pattern: re.Pattern) -> list[Group]:
    by_sub: dict[str, list[Command]] = {}
    for c in commands.read(s.logdir):
        by_sub.setdefault(c.subid, []).append(c)
    groups = []
    for sub in capture.subids(s.logdir):
        cap = capture.open_capture(s.logdir, sub)
        if cap is not None:
            groups.extend(search_capture(s, cap, by_sub.get(sub, []), pattern))
    return groups


def format_groups(groups: Iterable[Group], many_sessions: bool) -> Iterable[str]:
    last_head = None
    for g in groups:
        head = f"{g.session}  {g.subid}" if many_sessions else g.subid
        if head != last_head:
            if last_head is not None:
                yield ""
            yield f"== {head}"
            last_head = head
        if g.command:
            c = g.command
            yield (
                f"{timefmt.show(c.start, ms=True)}  $ {c.cmd.replace(chr(10), ' ⏎ ')}"
                f"    [exit {c.exit}, {c.cwd}]"
            )
        for h in g.hits:
            tag = "[screen] " if h.kind == "s" else ""
            yield f"{timefmt.show(h.t)}  > {tag}{h.text}"
