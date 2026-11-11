#!/usr/bin/env python3
"""Disable superseded prompt hooks in the image's optional personal zsh snapshot."""
from pathlib import Path
import sys

GUARD = '[[ ${POMDOCK_KIND:-} != container && ! -r /usr/local/lib/pomdock/prompt/prompt.zsh ]]'
MARKER = '# pomdock-owned prompt: suppress legacy network polling'


def guard(text):
    text = text.replace('if command -v starship &> /dev/null; then',
                        f'if {GUARD} && command -v starship &> /dev/null; then')
    line = 'autoload -Uz add-zsh-hook 2>/dev/null && add-zsh-hook precmd _pomdock_exitip'
    if MARKER not in text and line in text:
        text = text.replace(line, f'{MARKER}\nif {GUARD}; then\n{line}\nfi')
    return text


def main():
    for argument in sys.argv[1:]:
        path = Path(argument)
        if not path.is_file():
            continue
        original = path.read_text()
        changed = guard(original)
        if changed != original:
            path.write_text(changed)


if __name__ == '__main__':
    main()
