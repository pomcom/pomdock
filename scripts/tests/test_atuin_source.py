"""The shipped workspace must resolve actual Rust resources, not only manifests."""
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'vendor/atuin'
EXCLUDED = {'.git', 'target', 'node_modules', '.cache', '__pycache__'}
INCLUDE = re.compile(r'include_(str|bytes)!\s*\(\s*"([^"\n]+)"\s*\)')


class AtuinSourceTests(unittest.TestCase):
    def test_workspace_resources_and_build_inputs_survive_staging(self):
        with tempfile.TemporaryDirectory(prefix='pomdock-staged-rust-') as directory:
            staged = Path(directory) / 'atuin'
            subprocess.run(['bash', str(ROOT / 'scripts/stage-atuin.sh'), str(SOURCE), str(staged)],
                           check=True, capture_output=True)
            # Full tree copying preserves build.rs, protobufs, migrations, templates,
            # extensionless include data and any future resources within crates.
            originals = [p for p in (SOURCE / 'crates').rglob('*')
                         if p.is_file() and not EXCLUDED.intersection(p.relative_to(SOURCE).parts)]
            self.assertGreater(len(originals), 100)
            for original in originals:
                copied = staged / original.relative_to(SOURCE)
                self.assertTrue(copied.is_file(), str(original.relative_to(SOURCE)))
                self.assertEqual(copied.read_bytes(), original.read_bytes(), str(original))
            for copied in (staged / 'crates').rglob('*'):
                if copied.is_symlink():
                    resolved = copied.resolve(strict=True)
                    self.assertTrue(resolved.is_relative_to(staged), str(copied))
            resources = []
            for original in originals:
                if original.suffix != '.rs':
                    continue
                for kind, relative in INCLUDE.findall(original.read_text()):
                    resource = staged / original.relative_to(SOURCE).parent / relative
                    self.assertTrue(resource.is_file(), str(resource))
                    self.assertEqual(resource.read_bytes(), (original.parent / relative).read_bytes())
                    resources.append((kind, resource))
            self.assertGreaterEqual(len(resources), 10)
            contributors = staged / 'crates/atuin/src/command/CONTRIBUTORS'
            self.assertEqual(contributors.read_bytes(), (SOURCE / 'CONTRIBUTORS').read_bytes())
            self.assertTrue((staged / 'crates/atuin-daemon/proto/history.proto').is_file())
            self.assertTrue((staged / 'crates/atuin-server-postgres/migrations').is_dir())
            self.assertFalse((staged / 'target').exists())
            # Compile only a tiny resource harness, without dependencies/downloads.
            if rustc := shutil.which('rustc'):
                harness = Path(directory) / 'resources.rs'
                harness.write_text('\n'.join(
                    f'const _: &{ "str" if kind == "str" else "[u8]" } = include_{kind}!({json.dumps(str(path))});'
                    for kind, path in resources))
                subprocess.run([rustc, '--crate-type', 'lib', '--emit', 'metadata',
                                '-o', str(Path(directory) / 'resources.rmeta'), str(harness)],
                               check=True, capture_output=True)
