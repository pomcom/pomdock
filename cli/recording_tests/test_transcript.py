from recording import transcript
from recording.capture import Capture
from recording.transcript import Transcriber

from .conftest import write_capture


def run(chunks, cols=40, rows=10):
    tr = Transcriber(cols, rows)
    out = []
    for t, data in chunks:
        out += tr.feed(t, data)
    out += tr.finish()
    return [(ln.t, ln.kind, ln.text) for ln in out]


def texts(chunks):
    return [text for _, kind, text in run(chunks) if kind == "o"]


def test_line_gets_the_time_of_its_newline():
    got = run([(1.0, b"\x1b[1;32mroot\x1b[0m# who"), (1.5, b"ami\r\n"), (2.0, b"root\r\n")])
    assert got == [(1.5, "o", "root# whoami"), (2.0, "o", "root")]


def test_carriage_returns_backspaces_and_erase():
    assert texts([(0, b"progress 10%\rprogress 100%\r\n")]) == ["progress 100%"]
    assert texts([(0, b"abc\x08\x08XY\r\n")]) == ["aXY"]
    assert texts([(0, b"long line\r\x1b[Kshort\r\n")]) == ["short"]
    assert texts([(0, b"abcdef\x1b[3D\x1b[P\r\n")]) == ["abcef"]


def test_escape_sequences_and_utf8_split_across_writes():
    got = texts([(0, b"\x1b]0;tit"), (1, b"le\x07a\x1b[3"), (2, b"1mb\xe2\x9c"), (3, b"\x93\r\n")])
    assert got == ["ab✓"]


def test_tmux_window_names_are_metadata_even_when_split_across_writes():
    data = b"\x1b]2;command title\x1b\\\x1bkprintf\x1b\\printf\r\n"
    for split in range(len(data) + 1):
        assert texts([(0, data[:split]), (1, data[split:])]) == ["printf"]
    assert texts([(i, bytes([value])) for i, value in enumerate(data)]) == ["printf"]


def test_tmux_window_name_does_not_appear_in_full_screen_output():
    got = run([(0, b"\x1b[?1049h\x1b[H\x1bksecret-window-title\x1b\\visible\x1b[?1049l")])
    assert any(text == "visible" for _, kind, text in got if kind == "s")
    assert not any("secret-window-title" in text for _, _, text in got)


def test_full_screen_program_becomes_a_snapshot():
    got = run(
        [
            (1, b"$ vim f\r\n"),
            (2, b"\x1b[?1049h\x1b[2J\x1b[Hline one\r\n~\x1b[5;10Hstatus"),
            (3, b"\x1b[?1049l$ next\r\n"),
        ]
    )
    assert got[0] == (1, "o", "$ vim f")
    assert got[1] == (3, "S", "")
    assert (2, "s", "line one") in got
    assert (2, "s", "         status") in got
    assert got[-1] == (3, "o", "$ next")


def test_private_sequences_in_a_full_screen_program():
    """Device status queries with '?' crashed pyte (private=True to a handler without it)."""
    got = run([(1, b"\x1b[?1049hhello\x1b[?6n\x1b[?1A\x1b[?5;1H\x1b[?1049l")])
    assert (1, "s", "hello") in got


def test_unfinished_screen_and_line_are_flushed_at_the_end():
    got = run(
        [
            (1, b"\x1b[?1049hhtop"),
        ]
    )
    assert [k for _, k, _ in got] == ["S", "s"]
    assert texts([(1, b"no newline")]) == ["no newline"]


def test_cache_is_rebuilt_when_the_capture_grows(session, datadir):
    sub = "20261029T100000Z-main-pid1"
    write_capture(session, sub, [(0.5, b"one\r\n")], done=False)
    cache = session.parent / "cache"
    cap = Capture(session, sub)
    assert [ln.text for ln in transcript.lines(cache, cap)] == ["one"]
    write_capture(session, sub, [(0.5, b"one\r\n"), (0.5, b"two\r\n")])
    assert [ln.text for ln in transcript.lines(cache, Capture(session, sub))] == [
        "one",
        "two",
    ]
    assert not (session / f"{sub}.tsv").exists()  # nothing is written into logs/


def test_infer_size():
    from recording.transcript import infer_size

    assert infer_size("abc", 80, 24) == (80, 24)
    assert infer_size(" " * 140 + "\r\r$ ", 80, 24) == (140, 24)
    assert infer_size("\x1b[1;40r\x1b[10;120H", 80, 24) == (120, 40)
    assert infer_size("\x1b[28A", 80, 24) == (80, 29)
    assert infer_size("\x1b[999B", 80, 24) == (80, 24)


def test_command_line_wider_than_the_terminal():
    """zsh's redraw of a wrapped command line: it forces the wrap with a space, clears it
    with a carriage return and erase, which act on the new row, and goes on there. The
    line must come out whole, or searching for the command finds nothing."""
    tr = Transcriber(22, 5)
    # the first row (22 columns) is full at "close", the space wraps; then the new row is
    # rewritten
    out = tr.feed(1, b"# for p; do echo close") + tr.feed(
        2, b" \r\x1b[Kd\rd; done\x1b[?2004l\r\r\nout\r\n"
    )
    assert [(ln.kind, ln.text) for ln in out] == [
        ("c", "# for p; do echo closed; done"),
        ("o", "out"),
    ]


def test_cursor_up_redraws_an_earlier_row():
    tr = Transcriber(10, 5)
    out = tr.feed(1, b"0123456789abc\x1b[A\rXY\x1b[B\r\n")
    assert [ln.text for ln in out] == ["XY23456789abc"]


def test_parallel_requests_build_a_cache_once(session, datadir):
    """kalico show serves searches in parallel threads; they used to build the same
    cache at once and trip over each other's temporary files."""
    import threading

    from recording import cast
    from recording.session import Session

    sub = "20261029T100000Z-main-pid1"
    write_capture(session, sub, [(0.01, b"line %d\r\n" % i) for i in range(20000)])
    s = Session("acme", session, session.parent / "cache")
    errors, texts = [], []

    def work():
        try:
            cap = Capture(session, sub)
            texts.append(len(list(transcript.lines(s.cache, cap))))
            cast.build(s, cap)
        except Exception as e:  # noqa: BLE001 (collected and asserted below)
            errors.append(e)

    threads = [threading.Thread(target=work) for _ in range(8)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    assert errors == [] and texts == [20000] * 8
    assert not list(s.cache.glob("*tmp*"))  # no temporary files left behind


def test_damaged_cache_rows_are_skipped(session, datadir):
    sub = "20261029T100000Z-main-pid1"
    write_capture(session, sub, [(0.5, b"one\r\n"), (0.5, b"two\r\n")])
    cap = Capture(session, sub)
    cache = session.parent / "cache"
    tsv = transcript.update_cache(cache, cap)
    stamp, *rows = tsv.read_text().splitlines(keepends=True)
    tsv.write_text(stamp + rows[0] + "\x00" * 50 + "\n" + rows[1])
    assert [ln.text for ln in transcript.lines(cache, cap)] == ["one", "two"]


def test_cache_rebuilds_when_timing_catches_up_without_output_growth(session):
    sub = '20261106T100000Z-main-pid1'
    write_capture(session, sub, [(0.1,b'one\r\n'),(0.1,b'two\r\n')],done=False)
    timing=session/f'{sub}.timing'
    timing.write_text('0.100000 5\n')
    cache=session.parent/'cache'
    assert [ln.text for ln in transcript.lines(cache,Capture(session,sub))]==['one']
    timing.write_text('0.100000 5\n0.100000 5\n')
    assert [ln.text for ln in transcript.lines(cache,Capture(session,sub))]==['one','two']
