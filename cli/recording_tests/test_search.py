import json
import re
import time
from datetime import UTC, datetime

from recording import search
from recording.session import Session

from .conftest import write_capture

SUB = "20261029T100000Z-main-pid45"


def cmd(start, end, text, exit=0):
    return json.dumps(
        {
            "v": 2,
            "start": start,
            "end": end,
            "exit": exit,
            "cwd": "/root",
            "session": "acme",
            "subid": SUB,
            "pid": 45,
            "shell": "zsh",
            "cmd": text,
        }
    )


def setup(session):
    # capture started at 10:00:00.600; a prompt, two commands with output
    write_capture(
        session,
        SUB,
        [
            (0.1, b"# "),
            (1.0, b"nmap -sV host\r\n"),  # Enter at 10:00:01.700
            (0.5, b"22/tcp open ssh\r\n"),  # 10:00:02.200
            (0.3, b"# "),  # prompt at 10:00:02.500
            (2.0, b"whoami\r\n"),  # 10:00:04.500
            (0.1, b"root\r\n# "),  # 10:00:04.600
        ],
        precise="2026-10-29T10:00:00.600Z",
    )
    (session / "commands.jsonl").write_text(
        "\n".join(
            [
                cmd("2026-10-29T10:00:01.705Z", "2026-10-29T10:00:02.400Z", "nmap -sV host"),
                cmd("2026-10-29T10:00:04.505Z", "2026-10-29T10:00:04.550Z", "whoami"),
            ]
        )
        + "\n"
    )


def test_hits_are_timed_and_grouped_under_their_command(session):
    setup(session)
    groups = search.search_session(Session("acme", session, session.parent / "cache"), re.compile("open|root"))
    assert [(g.command.cmd if g.command else None, [h.text for h in g.hits]) for g in groups] == [
        ("nmap -sV host", ["22/tcp open ssh"]),
        ("whoami", ["root"]),
    ]
    assert groups[0].hits[0].t == datetime(2026, 10, 29, 10, 0, 2, 200000, UTC)


def test_command_line_itself_belongs_to_its_command(session):
    setup(session)
    groups = search.search_session(Session("acme", session, session.parent / "cache"), re.compile("nmap"))
    assert groups[0].command.cmd == "nmap -sV host"
    assert groups[0].hits[0].text == "# nmap -sV host"


def test_format(session, monkeypatch):
    monkeypatch.setenv("TZ", "UTC")  # output is in local time; make that UTC here
    time.tzset()
    setup(session)
    groups = search.search_session(Session("acme", session, session.parent / "cache"), re.compile("open"))
    out = list(search.format_groups(groups, many_sessions=False))
    monkeypatch.undo()
    time.tzset()
    assert out == [
        f"== {SUB}",
        "2026-10-29 10:00:01.705 UTC  $ nmap -sV host    [exit 0, /root]",
        "2026-10-29 10:00:02 UTC  > 22/tcp open ssh",
    ]


def test_output_is_not_assigned_to_a_future_command(session):
    setup(session)
    # Merely being within the input-echo allowance must not move output to a
    # command whose start is still in the future.
    from recording import commands
    from recording.commands import Command
    from recording.search import search_capture
    from recording.capture import Capture
    from datetime import timedelta
    cap = Capture(session, SUB)
    first = commands.read(session)[0]
    following = Command(first.start + timedelta(seconds=2.1), None, first.session,
                        first.subid, first.pid, None, first.cwd, 'future-command')
    groups = search_capture(Session('acme', session, session.parent/'cache'),cap,
                            [first,following],re.compile('22/tcp'))
    assert all(g.command is not following for g in groups)
