"""Timestamps: files use UTC, output shows local time."""

from datetime import UTC, datetime

_FORMATS = ("%Y-%m-%dT%H:%M:%S.%fZ", "%Y-%m-%dT%H:%M:%SZ", "%Y%m%dT%H%M%SZ")


def now_utc() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def parse(ts: str) -> datetime | None:
    """2026-10-12T16:51:40Z, 2026-10-12T16:51:40.123Z or 20261012T165140Z -> aware UTC."""
    for fmt in _FORMATS:
        try:
            return datetime.strptime(ts, fmt).replace(tzinfo=UTC)
        except ValueError:
            continue
    return None


def parse_docker(ts: str) -> datetime | None:
    """Docker's RFC 3339 times with nanoseconds, e.g. 2026-10-12T16:51:40.123456789Z."""
    head, _, _ = ts.partition(".")
    return parse(head.rstrip("Z") + "Z") if ts else None


def show(when: str | datetime, ms: bool = False) -> str:
    """Format for display: local time with the zone name. Unparseable input is returned
    unchanged."""
    dt = parse(when) if isinstance(when, str) else when
    if dt is None:
        return str(when)
    dt = dt.astimezone()
    out = dt.strftime("%Y-%m-%d %H:%M:%S")
    if ms:
        out += f".{dt.microsecond // 1000:03d}"
    return f"{out} {dt.tzname()}"
