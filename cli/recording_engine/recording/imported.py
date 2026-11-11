"""Search existing parsed paste imports with the recording engine's pattern."""


def search_groups(documents, pattern, limit=2000):
    groups, count = [], 0
    for document in documents:
        if document.get('source') != 'imported-paste' or document.get('deleted'):
            continue
        sub, title = document['id'], document.get('title', '')
        for entry in document.get('entries') or []:
            if entry.get('deleted'):
                continue
            command = entry.get('command', '')
            command_hit = bool(pattern.search(command))
            matching = [line for line in entry.get('output', '').splitlines() if pattern.search(line)]
            if not command_hit and not matching:
                continue
            if count >= limit:
                return dict(groups=groups, truncated=True)
            # Imported entries carry an optional real timestamp, never a guessed
            # duration, exit status or terminal replay position.
            timestamp = int(entry.get('ts', 0)) // 1_000_000
            allowed = max(0, limit - count - int(command_hit))
            visible = matching[:allowed]
            groups.append(dict(
                sub=sub, sessionId=sub, label=title,
                command=dict(id=entry['id'], sub=sub, sessionId=sub, label=title,
                             t=timestamp, end=None, exit=None,
                             cwd=entry.get('cwd', ''), cmd=command),
                commandHit=command_hit,
                hits=[dict(t=timestamp, text=line, screen=False) for line in visible]))
            count += len(visible) + int(command_hit)
            if len(visible) < len(matching):
                return dict(groups=groups, truncated=True)
    return dict(groups=groups, truncated=False)
