"""JSON protocol for Pomdock's Go API; never starts Docker or an HTTP server."""
import hashlib
import json
import re
import sys
from pathlib import Path
from . import capture, cast, commands, search, timeline, transcript
from .session import Session
from .imported import search_groups


def label(sub):
    match = re.match(r'\d+T\d+Z-(.+)-pid\d+$', sub)
    context = match[1] if match else sub
    return 'tmux ' + context[5:] if context.startswith('tmux-') else context


def key(c):
    identity = f'{c.subid}\0{c.record_id}' if c.record_id else f'{c.subid}\0{c.start.isoformat()}\0{c.pid}\0{c.pos}\0{c.cmd}'
    return hashlib.sha256(identity.encode()).hexdigest()[:32]


def row(c):
    return dict(id=key(c), sub=c.subid, sessionId=c.session_id or c.subid, tag=c.tag, label=label(c.subid), t=round(c.start.timestamp()*1000),
                end=round(c.end.timestamp()*1000) if c.end else None, exit=c.exit,
                cwd=c.cwd, cmd=c.cmd)


def entries(s, sub, indexed=None):
    cap = capture.open_capture(s.logdir, sub)
    if cap is None:
        return []
    cmds = [c for c in (indexed if indexed is not None else commands.with_sections(s.logdir)[0]) if c.subid == sub]
    lines = list(transcript.lines(s.cache, cap))
    clock = timeline.clock(s.cache, s.logdir, cap)
    raw, spans = capture.boundaries(cap)
    result = []
    for c in cmds:
        start, end = c.start.timestamp(), c.end.timestamp() if c.end else float('inf')
        # Shell hooks delimit the command's output using recorded time anchors.
        output = [line.text for line in lines if line.kind in ('o', 's') and
                  start <= clock.real(line.t) <= end]
        item = row(c)
        if c.record_id in spans:
            lo, hi = spans[c.record_id]
            if hi is None:
                hi = raw.rfind(b"\nScript done on ") if cap.finished() else len(raw)
                if hi < lo:
                    hi = len(raw)
            # Zsh emits its styled end-of-line/prompt padding before precmd.
            # Remove that exact control sequence, never an ordinary '%' output.
            span = re.sub(rb"\x1b\[1m\x1b\[7m[%#]\x1b\[27m\x1b\[1m\x1b\[0m {8,}\r ?\r$", b"", raw[lo:hi])
            parser = transcript.Transcriber(*cap.size())
            rendered = parser.feed(0, span) + parser.finish()
            output = [ln.text for ln in rendered if ln.kind in ('o','s','c')]
        item['output'] = '\n'.join(output)
        result.append(item)
    return result



def curated(c, edits):
    """Overlay curation without changing the source command or its identity."""
    result = row(c)
    hidden, changes = False, {}
    for ident in dict.fromkeys((c.subid, c.session_id or c.subid)):
        edit = edits.get(ident, {})
        change = edit.get('entries', {}).get(key(c), {})
        hidden = hidden or edit.get('deleted', False) or change.get('deleted', False)
        changes.update(change)
    if hidden:
        return None, changes
    if 'command' in changes:
        result['cmd'], result['edited'] = changes['command'], True
    if 'output' in changes:
        result['edited'] = True
    return result, changes


def output_hits(c, changes, pattern):
    return [dict(t=round(c.start.timestamp()*1000), text=line, screen=False)
            for line in changes.get('output', '').splitlines() if pattern.search(line)]


def run(req):
    s = Session(req['name'], Path(req['logdir']), Path(req['cache']))
    action = req['action']
    if action == 'sections':
        sections = [part for sub in capture.subids(s.logdir) if not req.get('sub') or sub == req['sub']
                    for part in commands.sections(s.logdir, sub)]
        return dict(sections=[{k:v for k,v in part.items() if k not in ('pos','endPos')} for part in sections])
    indexed, sections = commands.with_sections(s.logdir, req.get("sub")) if action in ("timeline", "search", "entries") else ([], [])
    by_id = {key(c): c for c in indexed}
    if action == 'timeline':
        available = set(capture.subids(s.logdir))
        return dict(commands=[row(c) for c in indexed if c.subid in available], shells=[dict(
            sub=sub, label=label(sub), bytes=cap.nbytes(), big=cap.nbytes()>300<<20,
            start=round(cap.start().timestamp()*1000) if cap.start() else None,
            finished=cap.finished(), background=capture.is_background(sub))
            for sub in capture.subids(s.logdir)
            if (cap := capture.open_capture(s.logdir, sub)) is not None])
    if action == 'search':
        try:
            pattern = re.compile(req.get('query', '') if req.get('regex') else re.escape(req.get('query', '')),
                                 0 if req.get('case') else re.IGNORECASE)
        except re.error as exc:
            return dict(groups=[], error=str(exc), truncated=False)
        groups, seen, hits = [], set(), 0
        edits = req.get('edits') or {}
        for g in search.search_session(s, pattern):
            command_row, changes = None, {}
            if g.command:
                g.command = by_id.get(key(g.command), g.command)
                command_row, changes = curated(g.command, edits)
                if command_row is None:
                    continue
                seen.add(key(g.command))
            elif edits.get(g.subid, {}).get('deleted'):
                continue
            hit = bool(command_row and pattern.search(command_row['cmd']))
            output = [dict(t=round(h.t.timestamp()*1000), text=h.text, screen=h.kind=='s')
                      for h in g.hits if not (h.kind == 'c' and (hit or 'command' in changes))]
            if 'output' in changes:
                output = output_hits(g.command, changes, pattern)
            if not hit and not output:
                continue
            groups.append(dict(sub=g.subid, sessionId=g.command.session_id if g.command else g.subid,
                               tag=g.command.tag if g.command else '', label=label(g.subid),
                               command=command_row, commandHit=hit, hits=output))
            hits += len(output)+int(hit)
            if hits >= 2000:
                return dict(groups=groups, truncated=True)
        for c in indexed:
            if key(c) in seen:
                continue
            command_row, changes = curated(c, edits)
            if command_row is None:
                continue
            hit = bool(pattern.search(command_row['cmd']))
            output = output_hits(c, changes, pattern) if 'output' in changes else []
            if hit or output:
                groups.append(dict(sub=c.subid, sessionId=c.session_id or c.subid, tag=c.tag,
                                   label=label(c.subid), command=command_row, commandHit=hit, hits=output))
                hits += len(output)+int(hit)
                if hits >= 2000:
                    return dict(groups=groups, truncated=True)
        imported = search_groups(req.get('imports') or [], pattern, limit=max(0, 2000-hits))
        groups.extend(imported['groups'])
        groups.sort(key=lambda g: g['command']['t'] if g['command'] else g['hits'][0]['t'])
        return dict(groups=groups, truncated=imported['truncated'])
    sub = req.get('sub', '')
    if '/' in sub or '\\' in sub or sub in ('', '.', '..'):
        raise ValueError('invalid recording ID')
    cap = capture.open_capture(s.logdir, sub)
    if cap is None:
        raise ValueError('recording not found')
    if action == 'info':
        data = cast.info(s, cap)
        data['revision'] = cap.signature()
        return data
    if action == 'cast':
        path, _ = cast.build(s, cap)
        return dict(path=str(path))
    if action == 'transcript':
        clock = timeline.clock(s.cache, s.logdir, cap)
        return dict(lines=[dict(t=round(clock.real(line.t)*1000), kind=line.kind, text=line.text)
                           for line in transcript.lines(s.cache, cap)])
    if action == 'entries':
        return dict(entries=entries(s, sub, indexed), sections=[{k:v for k,v in part.items() if k not in ('pos','endPos')} for part in sections if part['captureId'] == sub])
    raise ValueError('unknown recording operation')


def main():
    try:
        print(json.dumps(run(json.load(sys.stdin)), ensure_ascii=True))
    except Exception as exc:
        print(json.dumps(dict(error=f'{type(exc).__name__}: {exc}')))
        sys.exit(1)
