from dataclasses import dataclass
from pathlib import Path
@dataclass
class Session:
    name: str
    logdir: Path
    cache: Path
