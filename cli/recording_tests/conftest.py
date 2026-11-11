import json
from pathlib import Path

import pytest


def write_capture(
    logdir: Path,
    sub: str,
    chunks: list[tuple[float, bytes]],
    *,
    start: str = "2026-10-29 10:00:00+00:00",
    precise: str | None = None,
    cols: int = 80,
    rows: int = 24,
    done: bool = True,
) -> None:
    """A script(1) capture in the classic timing format: chunks are (delay, data)."""
    header = (
        f'Script started on {start} [COMMAND="exec /usr/bin/zsh" TERM="xterm" '
        f'TTY="/dev/pts/1" COLUMNS="{cols}" LINES="{rows}"]\n'
    ).encode()
    body = b"".join(d for _, d in chunks)
    trailer = b'\nScript done on 2026-10-29 10:10:00+00:00 [COMMAND_EXIT_CODE="0"]\n'
    (logdir / f"{sub}.log").write_bytes(header + body + (trailer if done else b""))
    (logdir / f"{sub}.timing").write_text("".join(f"{d:.6f} {len(c)}\n" for d, c in chunks))
    if precise:
        (logdir / f"{sub}.start").write_text(precise + "\n")


@pytest.fixture
def datadir(tmp_path):
    return tmp_path


@pytest.fixture
def session(datadir):
    """A session folder 'acme' with meta.json and no captures yet."""
    logdir = datadir / "acme" / "logs"
    logdir.mkdir(parents=True)
    (logdir / "meta.json").write_text(
        json.dumps({"name": "acme", "created": "2026-10-29T10:00:00Z"})
    )
    return logdir
