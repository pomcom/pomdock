"""The command index: one record per command line typed at a shell prompt.

legacy: format 1, commands.log, tab-separated, written by older images:
  <start UTC>  <session>  <subid>  <shell pid>  <exit>  <cwd>  <command>
with tabs, newlines and backslashes in the command escaped as \\t, \\n and \\\\.

Format 2, commands.jsonl, one JSON object per line:
  {"v": 2, "start": ..., "end": ..., "exit": 0, "cwd": ..., "session": ..., "subid": ...,
   "pid": 45, "shell": "zsh", "pos": 1234, "end_pos": 2345, "cmd": ...}
start and end are UTC with milliseconds; end is when the prompt returned. pos and
end_pos (newer images) are the size of the shell's capture at those moments.
"""

import json
import re
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from . import timefmt

V1 = "commands.log"
V2 = "commands.jsonl"


@dataclass
class Command:
    start: datetime
    end: datetime | None
    session: str
    subid: str
    pid: int
    exit: int | None
    cwd: str
    cmd: str
    pos: int | None = None  # bytes into the shell's capture when it started
    end_pos: int | None = None  # ... and when it ended
    record_id: str = ""
    session_id: str = ""
    tag: str = ""


def _unescape_v1(s: str) -> str:
    return re.sub(r"\\(.)", lambda m: {"t": "\t", "n": "\n"}.get(m.group(1), m.group(1)), s)


def _int(s) -> int:
    try:
        return int(s)
    except (TypeError, ValueError):
        return -1


def _read_v1(path: Path) -> Iterator[Command]:
    with open(path, encoding="utf-8", errors="replace") as f:
        for row in f:
            parts = row.rstrip("\n").split("\t", 6)
            if len(parts) != 7:
                continue
            start = timefmt.parse(parts[0])
            if start is None:
                continue
            yield Command(
                start,
                None,
                parts[1],
                parts[2],
                _int(parts[3]),
                _int(parts[4]),
                parts[5],
                _unescape_v1(parts[6]),
            )


def _read_v2(path: Path) -> Iterator[Command]:
    records = {}
    with open(path, encoding="utf-8", errors="replace") as f:
        for row in f:
            try:
                r = json.loads(row)
            except ValueError:
                continue  # e.g. a line cut off when the container was killed
            if not isinstance(r, dict):
                continue
            ident = r.get("id")
            if ident:
                records.setdefault(ident, {}).update(r)
            else:
                records[len(records)] = r
    for r in records.values():
        start = timefmt.parse(str(r.get("start", "")))
        if start is None:
            continue
        yield Command(start, timefmt.parse(str(r.get("end", ""))),
            str(r.get("session", "")), str(r.get("subid", "")), _int(r.get("pid")),
            _int(r.get("exit")) if r.get("exit") is not None else None,
            str(r.get("cwd", "")), str(r.get("cmd", "")),
            r.get("pos") if isinstance(r.get("pos"), int) else None,
            r.get("end_pos") if isinstance(r.get("end_pos"), int) else None,
            str(r.get("id", "")))


def read(logdir: Path) -> list[Command]:
    """All commands of a session in both formats, by start time."""
    out: list[Command] = []
    if (logdir / V1).is_file():  # legacy
        out.extend(_read_v1(logdir / V1))
    if (logdir / V2).is_file():
        out.extend(_read_v2(logdir / V2))
    for snapshot in sorted(logdir.glob("*.commands.jsonl")):
        out.extend(_read_v2(snapshot))
    unique = {}
    for c in out:
        identity = (c.subid, c.record_id) if c.record_id else (c.subid, c.start, c.pid, c.pos, c.cmd)
        previous = unique.get(identity)
        if previous is not None and previous.end is not None and c.end is None:
            continue
        unique[identity] = c
    return sorted(unique.values(), key=lambda c: c.start)


# pdsession writes these literal markers into the immutable terminal capture.
# Positions, rather than rounded marker timestamps, decide command ownership.
_SECTION = re.compile(rb"(?m)^[^\r\n]*?###POMDOCK-SESSION::([^\r\n]*?)::(\d+)###\r?$")
_ESCAPE = re.compile(rb"\x1b(?:\][^\x07\x1b]*(?:\x07|\x1b\\)|\[[0-?]*[ -/]*[@-~]|[PX^_k].*?\x1b\\)", re.S)


def sections(logdir: Path, sub: str, cap=None) -> list[dict]:
    from . import capture
    cap = cap or capture.open_capture(logdir, sub)
    if cap is None:
        return []
    raw, _ = capture.boundaries(cap)
    markers = list(_SECTION.finditer(raw))
    begin = round(cap.start().timestamp()*1000) if cap.start() else None
    result = []
    if not markers:
        return [dict(id=sub, captureId=sub, tag='', start=begin, pos=0, endPos=len(raw))]
    pre = raw[len(cap.header()):markers[0].start()]
    if _ESCAPE.sub(b'', pre).strip():
        result.append(dict(id='', captureId=sub, tag='', start=begin, pos=0))
    for marker in markers:
        result.append(dict(id='', captureId=sub, tag=marker[1].decode('utf-8', 'replace').strip(),
                           start=int(marker[2])*1000, pos=marker.end()))
    for i, section in enumerate(result):
        section['id'] = f'{sub}#{i}'
        section['endPos'] = result[i+1]['pos'] if i+1 < len(result) else len(raw)
        section['end'] = result[i+1]['start'] if i+1 < len(result) else None
    return result


def with_sections(logdir: Path, selected: str | None = None) -> tuple[list[Command], list[dict]]:
    from . import capture
    items = [c for c in read(logdir) if selected is None or c.subid == selected]
    all_sections = []
    for sub in capture.subids(logdir):
        if selected is not None and sub != selected:
            continue
        cap = capture.open_capture(logdir, sub)
        parts = sections(logdir, sub, cap)
        all_sections.extend(parts)
        _, spans = capture.boundaries(cap)
        for command in (c for c in items if c.subid == sub):
            pos = spans[command.record_id][0] if command.record_id in spans else command.pos
            candidates = [part for part in parts if
                          (pos is not None and part['pos'] <= pos) or
                          (pos is None and part['start'] is not None and part['start'] <= command.start.timestamp()*1000)]
            part = candidates[-1] if candidates else parts[0]
            command.session_id, command.tag = part['id'], part['tag']
    return items, all_sections
