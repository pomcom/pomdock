import json
import re
from datetime import UTC, datetime

from recording import search, timeline, transcript
from recording.capture import Capture
from recording.session import Session

from .conftest import write_capture

SUB = "20261029T100000Z-main-pid45"


def record(start, text):
    return json.dumps({"v": 2, "start": start, "end": start, "exit": 0, "cwd": "/",
                       "session": "acme", "subid": SUB, "pid": 45, "shell": "zsh",
                       "cmd": text})  # fmt: skip


def slept(session):
    """Two commands 5 s apart in the capture, but the host slept an hour in between,
    which script(1) didn't record: the command index has the real times."""
    write_capture(
        session,
        SUB,
        [
            (1.0, b"# first\x1b[?2004l\r\r\nout-1\r\n# "),  # capture 1 s
            (0.5, b"\r\n# \x1b[?2004l\r\r\n# "),  # an empty Enter: no command
            (4.0, b"second\x1b[?2004l\r\r\nout-2\r\n# "),  # capture 5.5 s
        ],
        precise="2026-10-29T10:00:00.000Z",
    )
    (session / "commands.jsonl").write_text(
        record("2026-10-29T10:00:01.002Z", "first")
        + "\n"
        + record("2026-10-29T11:00:05.502Z", "second")
        + "\n"
    )
    return Capture(session, SUB)


def test_command_lines_are_marked_for_zsh_and_bash():
    tr = transcript.Transcriber()
    zsh = tr.feed(1, b"# echo hi\x1b[?2004l\r\r\nhi\r\n")
    bash = tr.feed(2, b"$ echo hi\r\n\x1b[?2004l\rhi\r\n")
    assert [(ln.kind, ln.text) for ln in zsh + bash] == [
        ("c", "# echo hi"), ("o", "hi"), ("c", "$ echo hi"), ("o", "hi"),
    ]  # fmt: skip


def test_clock_follows_the_real_time_across_a_sleep(session, datadir):
    cap = slept(session)
    clock = timeline.clock(session.parent / "cache", session, cap)
    assert len(clock.anchors) == 2
    real = datetime(2026, 10, 29, 11, 0, 5, 500000, tzinfo=UTC).timestamp()
    assert abs(clock.real(5.5) - real) < 0.01
    assert abs(clock.capture(real) - 5.5) < 0.01
    # a time during the sleep maps to the end of what was recorded before it
    during = datetime(2026, 10, 29, 10, 30, tzinfo=UTC).timestamp()
    assert 1.5 <= clock.capture(during) < 5.5


def test_search_shows_real_times_after_a_sleep(session):
    slept(session)
    groups = search.search_session(Session("acme", session, session.parent / "cache"), re.compile("out-2"))
    assert groups[0].command.cmd == "second"
    assert groups[0].hits[0].t.replace(microsecond=0) == datetime(
        2026, 10, 29, 11, 0, 5, tzinfo=UTC
    )


def test_anchors_never_put_the_capture_ahead_of_real_time():
    class C:
        def __init__(self, cmd, at):
            self.cmd, self.start = cmd, datetime.fromtimestamp(at, UTC)

    # 'y' ran 2 s in, real time; a line at capture time 100 s can't be it, since a
    # capture only ever falls behind (sleep), never ahead
    lines = [transcript.Line(1.0, "c", "# x"), transcript.Line(100.0, "c", "# y")]
    assert timeline.anchors(0.0, lines, [C("x", 1.0), C("y", 2.0)]) == [(1.0, 1.0)]
