import json
from datetime import UTC, datetime
from recording import bridge
from .conftest import write_capture


def marker(kind, ident):
    return f'\x1b]777;pomdock-{kind};{ident}\x07'.encode()


def tagged_capture(session):
    sub = '20261106T100000Z-main-pid1'
    epoch = int(datetime(2026, 11, 6, 10, tzinfo=UTC).timestamp())
    body = (f'###POMDOCK-SESSION::recon::{epoch}###\r\n'.encode() +
            marker('start', 'one') + b'RECON_RESULT\r\n' + marker('end', 'one') +
            f'\r\n###POMDOCK-SESSION::exploit::{epoch+1}###\r\n'.encode() +
            marker('start', 'two') + b'EXPLOIT_RESULT\r\n' + marker('end', 'two'))
    write_capture(session, sub, [(0.01, body)], precise='2026-11-06T10:00:00.000Z')
    records = [dict(v=2, id=ident, start=f'2026-11-06T10:00:0{i}.100Z',
                    end=f'2026-11-06T10:00:0{i}.900Z', exit=0, cwd='/pentest',
                    session='acme', subid=sub, pid=1, pos=0, end_pos=0, cmd=f'printf {tag}')
               for i, (ident, tag) in enumerate([('one', 'recon'), ('two', 'exploit')])]
    (session/'commands.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in records))
    return sub, dict(name='acme', logdir=str(session), cache=str(session.parent/'cache'))


def test_sections_preserve_raw_shell_and_command_identity(session):
    sub, request = tagged_capture(session)
    raw = (session/f'{sub}.log').read_bytes()
    timeline = bridge.run(dict(request, action='timeline'))
    assert [(c['sessionId'], c['tag']) for c in timeline['commands']] == [(sub+'#0', 'recon'), (sub+'#1', 'exploit')]
    assert all(c['sub'] == sub for c in timeline['commands'])
    assert timeline['shells'][0]['sub'] == sub
    metadata = bridge.run(dict(request, action='sections', sub=sub))['sections']
    assert [(s['id'], s['tag']) for s in metadata] == [(sub+'#0', 'recon'), (sub+'#1', 'exploit')]
    entries = bridge.run(dict(request, action='entries', sub=sub))['entries']
    assert [e['id'] for e in entries] == [c['id'] for c in timeline['commands']]
    assert [e['output'] for e in entries] == ['RECON_RESULT', 'EXPLOIT_RESULT']
    found = bridge.run(dict(request, action='search', query='EXPLOIT_RESULT'))['groups']
    assert len(found) == 1 and found[0]['sessionId'] == sub+'#1'
    assert (session/f'{sub}.log').read_bytes() == raw


def test_section_curation_does_not_hide_siblings_or_old_parent_edits(session):
    sub, request = tagged_capture(session)
    commands = bridge.run(dict(request, action='timeline'))['commands']
    one, two = [c['id'] for c in commands]
    edits = {sub: {'entries': {one: {'command': 'parent recon'}}},
             sub+'#0': {'entries': {one: {'output': 'CURATED_RECON'}}},
             sub+'#1': {'deleted': True}}
    assert bridge.run(dict(request, action='search', query='RECON_RESULT', edits=edits))['groups'] == []
    curated = bridge.run(dict(request, action='search', query='CURATED_RECON', edits=edits))['groups']
    assert len(curated) == 1 and curated[0]['command']['cmd'] == 'parent recon'
    assert bridge.run(dict(request, action='search', query='EXPLOIT_RESULT', edits=edits))['groups'] == []
    assert bridge.run(dict(request, action='search', query='printf exploit', edits=edits))['groups'] == []
    original = bridge.run(dict(request, action='search', query='EXPLOIT_RESULT'))['groups']
    assert original[0]['command']['id'] == two


def test_transcript_uses_real_clock_and_visible_terminal_content(session):
    sub, request = tagged_capture(session)
    lines = bridge.run(dict(request, action='transcript', sub=sub))['lines']
    assert any(line['text'] == 'RECON_RESULT' for line in lines)
    assert all(isinstance(line['t'], int) and line['t'] > 0 for line in lines)
    assert all('\x1b' not in line['text'] for line in lines)
