import json
import re
from recording import bridge, search
from recording.session import Session
from .conftest import write_capture


def marker(kind, ident):
    return f'\x1b]777;pomdock-{kind};{ident}\x07'.encode()


def test_fast_commands_use_stream_boundaries_not_stale_file_sizes(session):
    sub='20261106T100000Z-main-pid1'
    body=(marker('start','one')+b'\x1bkprintf\x1b\\FIRST_RESULT\r\n'+b'\x1b[1m\x1b[7m%\x1b[27m\x1b[1m\x1b[0m'+b' '*79+b'\r \r'+marker('end','one')+
          marker('start','two')+b'\x1b[?1049h\x1b[HSECOND_SCREEN\x1b[?1049l'+marker('end','two'))
    # Both commands landed in a single script write. Their shell-hook file
    # sizes are deliberately stale, as can happen with very short commands.
    write_capture(session,sub,[(0.01,body)],precise='2026-11-06T10:00:00.000Z')
    records=[]
    for ident,cmd,start,end in [('one','printf first','00.000','01.000'),('two','less second','01.001','02.000')]:
        records.append(dict(v=2,id=ident,start=f'2026-11-06T10:00:{start}Z',
            end=f'2026-11-06T10:00:{end}Z',exit=0,cwd='/pentest',session='acme',
            subid=sub,pid=1,pos=0,end_pos=0,cmd=cmd))
    index=session/'commands.jsonl'
    index.write_text(''.join(json.dumps(r)+'\n' for r in records))
    raw=(session/f'{sub}.log').read_bytes()
    s=Session('acme',session,session.parent/'cache')
    entries=bridge.entries(s,sub)
    assert entries[0]['output']=='FIRST_RESULT'
    assert entries[1]['output']=='SECOND_SCREEN'
    groups=search.search_session(s,re.compile('SECOND_SCREEN'))
    assert len(groups)==1 and groups[0].command.cmd=='less second'
    assert (session/f'{sub}.log').read_bytes()==raw


def test_started_command_survives_missing_completion(session):
    from recording import commands
    (session/'commands.jsonl').write_text(json.dumps(dict(v=2,id='still-running',
        start='2026-11-06T10:00:00.000Z',subid='shell',pid=1,cmd='exec long-job',cwd='/pentest'))+'\n'+
        '{"v":2,"id":"incomplete')
    records=commands.read(session)
    assert len(records)==1 and records[0].cmd=='exec long-job'
    assert records[0].end is None and records[0].exit is None


def test_repeated_commands_with_identical_timestamps_keep_distinct_ids(session):
    from recording import commands
    rows=[dict(v=2,id=ident,start='2026-11-06T10:00:00.000Z',subid='shell',pid=1,
               pos=0,cwd='/pentest',cmd='whoami') for ident in ['one','two']]
    (session/'commands.jsonl').write_text(''.join(json.dumps(row)+'\n' for row in rows))
    found=commands.read(session)
    assert len(found)==2 and bridge.key(found[0])!=bridge.key(found[1])
