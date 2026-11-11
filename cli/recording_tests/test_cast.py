import json
from datetime import UTC, datetime

from recording import cast, timeline
from recording.capture import Capture
from recording.session import Session

from .conftest import write_capture

SUB = "20261029T100000Z-main-pid45"


def record(start, end, text, pos=None, end_pos=None):
    r = {"v": 2, "start": start, "end": end, "exit": 0, "cwd": "/", "session": "acme",
         "subid": SUB, "pid": 45, "shell": "zsh", "cmd": text}  # fmt: skip
    if pos is not None:
        r.update(pos=pos, end_pos=end_pos)
    return json.dumps(r)


def recorded(session):
    """Two commands; 30 s of idle between them in the capture, and an hour of sleep the
    capture didn't see. As zsh does, bracketed paste is switched off on Enter; and as the
    hooks do, the command index has where each command starts and ends in the capture."""
    enter = b"\x1b[?2004l\r\r\n"
    chunks = [
        (1.0, b" " * 20 + b"\r\r# first" + enter),  # zsh's prompt marker: 20 columns
        (0.5, b"out-1\r\n"),
        (30.0, b" " * 30 + b"\r\r# second" + enter),  # window widened to 30 columns
        (0.5, b"out-2\r\n"),
    ]
    write_capture(session, SUB, chunks, cols=20, rows=5, precise="2026-10-29T10:00:00.000Z")
    ends, size = [], (session / f"{SUB}.log").read_bytes().index(b"\n") + 1
    for _, data in chunks:
        size += len(data)
        ends.append(size)
    (session / "commands.jsonl").write_text(
        record("2026-10-29T10:00:01.000Z", "2026-10-29T10:00:01.500Z", "first", ends[0], ends[1])
        + "\n"
        + record(
            "2026-10-29T11:00:31.500Z", "2026-10-29T11:00:32.000Z", "second", ends[2], ends[3]
        )  # fmt: skip
        + "\n"
    )
    return Capture(session, SUB)


def events(path):
    lines = path.read_text().splitlines()
    return json.loads(lines[0]), [json.loads(x) for x in lines[1:]]


def test_cast_is_asciicast_v2_with_shortened_pauses_and_resizes(session):
    cap = recorded(session)
    path, breaks = cast.build(Session("acme", session, session.parent / "cache"), cap)
    header, evs = events(path)
    assert header["version"] == 2 and (header["width"], header["height"]) == (20, 5)
    times = [e[0] for e in evs]
    assert times == sorted(times) and max(times) < 1.5 + cast.IDLE + 1  # 30 s became 2 s
    assert ["r", "30x5"] in [e[1:] for e in evs]
    assert "".join(e[2] for e in evs if e[1] == "o").count("out-") == 2
    assert breaks[-1] == (1.5 + cast.IDLE, 31.5)


def test_exact_anchors_from_recorded_positions(session, datadir):
    cap = recorded(session)
    clock = timeline.clock(Session("acme", session, session.parent / "cache").cache, session, cap)
    second = datetime(2026, 10, 29, 11, 0, 31, 500000, tzinfo=UTC).timestamp()
    assert (31.5, second) in clock.anchors  # no text matching involved
    assert abs(clock.real(32.0) - (second + 0.5)) < 0.001


def test_info_maps_cast_time_to_real_time_and_marks_commands(session):
    cap = recorded(session)
    info = cast.info(Session("acme", session, session.parent / "cache"), cap)
    labels = [m[1] for m in info["markers"]]
    assert labels[0].endswith("$ first") and labels[1].endswith("$ second")
    # the second command's marker sits at its cast time, and the table maps it to the
    # real time after the sleep
    ct = info["markers"][1][0] - cast.AFTER
    table = info["table"]
    point = max(p for p in table if p[0] <= ct)
    real_ms = point[1] + (ct - point[0]) * 1000
    want = datetime(2026, 10, 29, 11, 0, 31, 500000, tzinfo=UTC).timestamp() * 1000
    assert abs(real_ms - want) < 5


def test_cast_and_capture_time_map_onto_each_other():
    breaks = [(0.0, 0.0), (3.0, 31.0)]
    assert cast.cast_time(breaks, 1.0) == 1.0
    assert cast.cast_time(breaks, 20.0) == 3.0  # inside the shortened pause: its end
    assert cast.cast_time(breaks, 32.0) == 4.0
    assert cast.capture_time(breaks, 4.0) == 32.0
