#!/usr/bin/env python3
"""Record successful lifecycle events beside an engagement's logs, without touching Atuin."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def atomic_json(path, document):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, temporary = tempfile.mkstemp(prefix=path.name+".", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(document, stream, ensure_ascii=True, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def read_json(path, default):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return default


@contextmanager
def locked(path):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with path.open("a") as stream:
        os.chmod(path, 0o600)
        fcntl.flock(stream.fileno(), fcntl.LOCK_EX)
        yield


def inspect(kind, target):
    proc = subprocess.run(["docker", kind, "inspect", target], capture_output=True,
                          text=True, timeout=10, check=False)
    if proc.returncode:
        raise RuntimeError(f"cannot inspect {kind} {target}; lifecycle metadata was not updated")
    result = json.loads(proc.stdout)
    if not isinstance(result, list) or len(result) != 1 or not isinstance(result[0], dict):
        raise ValueError(f"invalid {kind} inspection result")
    return result[0]


def record(args):
    loot = Path(args.loot).expanduser().resolve()
    name = "(default)" if args.name == "pcm-pentest" else args.name
    if not name.strip() or any(ch in name for ch in "/\\\x00\r\n"):
        raise ValueError("invalid engagement name")
    if sys.platform == "darwin":
        config = Path.home()/"Library"/"Application Support"/"pomdock"
    else:
        config = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home()/".config")/"pomdock"
    directory = loot/".pomdock"
    manifest_path = directory/"engagement.json"
    image = None
    container = None
    # Read actual runtime identity before changing the manifest or registry.
    if args.event in ("create", "start"):
        container = inspect("container", args.container or args.name)
        image_id = container.get("Image")
        if not isinstance(image_id, str) or not image_id:
            raise ValueError("container inspection has no image ID")
        image = inspect("image", image_id)
    with locked(config/"engagement-roots.lock"):
        path = config/"engagement-roots.json"
        roots = read_json(path, {})
        if not isinstance(roots, dict):
            raise ValueError("invalid engagement roots registry")
        existing = roots.get(name)
        if existing and (not isinstance(existing, str) or Path(existing).expanduser().resolve() != loot):
            raise ValueError(f"engagement {name!r} is already registered at another loot directory")
        with locked(directory/"engagement.lock"):
            manifest = read_json(manifest_path, {})
            if not isinstance(manifest, dict) or (manifest and manifest.get("version") != 1):
                raise ValueError("unsupported engagement manifest")
            if manifest and manifest.get("name") != name:
                raise ValueError("loot directory belongs to another engagement")
            is_new = not manifest
            stamp = now()
            if args.event != "init" or is_new:
                manifest.update(version=1, name=name, backend="docker", updated=stamp)
                manifest.setdefault("created", stamp)
                manifest["container"] = args.container or args.name
                if container is not None:
                    container_id = container.get("Id")
                    if not isinstance(container_id, str) or not container_id:
                        raise ValueError("container inspection has no container ID")
                    manifest["containerId"] = container_id
                    labels = (image.get("Config") or {}).get("Labels") or {}
                    metadata = manifest.get("image")
                    if not isinstance(metadata, dict):
                        metadata = {}
                    metadata.update(reference=(container.get("Config") or {}).get("Image") or args.image,
                        id=container["Image"], revision=labels.get("org.opencontainers.image.revision", ""),
                        built=labels.get("org.opencontainers.image.created", ""),
                        toolset=labels.get("io.pomdock.toolset", ""),
                        recordingFormat=labels.get("io.pomdock.recording-format", ""))
                    manifest["image"] = metadata
                elif args.container_id:
                    manifest["containerId"] = args.container_id
                # Never invent an image ID or build metadata for init, stop or remove.
                elif not manifest.get("image") and args.image:
                    manifest["image"] = {"reference": args.image}
                atomic_json(manifest_path, manifest)
                with (directory/"events.jsonl").open("a", encoding="utf-8") as stream:
                    os.chmod(directory/"events.jsonl", 0o600)
                    event = dict(at=stamp, event=args.event, container=manifest["container"])
                    if manifest.get("containerId"):
                        event["containerId"] = manifest["containerId"]
                    if manifest.get("image"):
                        event["image"] = manifest["image"]
                    stream.write(json.dumps(event, ensure_ascii=True)+"\n")
                    stream.flush()
                    os.fsync(stream.fileno())
        roots[name] = str(loot)
        atomic_json(path, roots)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--loot", required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--event", required=True, choices=["init", "create", "start", "stop", "remove"])
    parser.add_argument("--container", default="")
    parser.add_argument("--container-id", default="")
    parser.add_argument("--image", default="")
    args = parser.parse_args()
    try:
        record(args)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
        print(f"pomdock: engagement metadata: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
