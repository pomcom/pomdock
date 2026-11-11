#!/usr/bin/env python3
"""Normalize image-owned home paths without merging or discarding existing data."""
import os
from pathlib import Path
import pwd
import sys


def normalize(home, uid, gid):
    workspace = home / 'workspace'
    legacy = home / 'pentest'
    if workspace.is_symlink() or (workspace.exists() and not workspace.is_dir()):
        raise ValueError('workspace must be a real directory')
    if legacy.is_symlink():
        if legacy.resolve() != workspace.resolve():
            raise ValueError('pentest symlink points outside workspace; refusing to replace it')
    elif legacy.exists():
        if not legacy.is_dir():
            raise ValueError('pentest must be a directory or workspace symlink')
        if not workspace.exists():
            legacy.rename(workspace)
        elif not any(legacy.iterdir()):
            legacy.rmdir()
        elif not any(workspace.iterdir()):
            workspace.rmdir()
            legacy.rename(workspace)
        else:
            raise ValueError('both workspace and pentest contain files; refusing to merge them')
    workspace.mkdir(mode=0o755, exist_ok=True)
    os.chown(workspace, uid, gid)
    if not legacy.is_symlink():
        legacy.symlink_to('workspace', target_is_directory=True)
        os.lchown(legacy, uid, gid)


def main():
    if len(sys.argv) != 2:
        raise ValueError('usage: workspace-layout.py USERNAME')
    user = pwd.getpwnam(sys.argv[1])
    normalize(Path(user.pw_dir), user.pw_uid, user.pw_gid)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError) as error:
        raise SystemExit(f'pomdock: {error}')
