"""Lifecycle metadata fixtures never contact a real Docker daemon."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("engagement_meta", Path(__file__).parents[1]/"engagement-meta.py")
META = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(META)


class EngagementMetadataTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.loot = self.root/"custom-loot"
        self.config = self.root/"config"
        self.env = patch.dict(os.environ, {"HOME": str(self.root/"home"), "XDG_CONFIG_HOME": str(self.config)})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.platform = patch.object(META.sys, "platform", "linux")
        self.platform.start()
        self.addCleanup(self.platform.stop)
        self.args = argparse.Namespace(loot=str(self.loot), name="fixture", event="init", container="fixture", container_id="", image="fixture:latest")

    def manifest(self):
        return json.loads((self.loot/".pomdock/engagement.json").read_text())

    def events(self):
        return [json.loads(line) for line in (self.loot/".pomdock/events.jsonl").read_text().splitlines()]

    def test_init_is_offline_idempotent_and_remembers_custom_root(self):
        with patch.object(META, "inspect", side_effect=AssertionError("init must work offline")):
            META.record(self.args)
            before = self.manifest()
            META.record(self.args)
        self.assertEqual(self.manifest(), before)
        self.assertEqual(len(self.events()), 1)
        self.assertEqual(before["image"], {"reference": "fixture:latest"})
        self.assertNotIn("containerId", before)
        roots = json.loads((self.config/"pomdock/engagement-roots.json").read_text())
        self.assertEqual(roots, {"fixture": str(self.loot)})
        self.assertEqual((self.loot/".pomdock/engagement.json").stat().st_mode & 0o777, 0o600)

    def test_start_records_actual_identity_and_preserves_existing_data(self):
        META.record(self.args)
        previous = self.manifest()
        previous["extra"] = {"keep": True}
        previous["image"]["extraImageField"] = "keep"
        META.atomic_json(self.loot/".pomdock/engagement.json", previous)
        atuin = self.loot/".atuin/history.db"
        atuin.parent.mkdir()
        atuin.write_bytes(b"unchanged history fixture")
        container = {"Id": "container-actual", "Image": "sha256:actual", "Config": {"Image": "fixture:pinned"}}
        image = {"Id": "sha256:actual", "Config": {"Labels": {"org.opencontainers.image.revision": "revision-fixture", "org.opencontainers.image.created": "2026-11-01T00:00:00Z", "io.pomdock.toolset": "full", "io.pomdock.recording-format": "3"}}}
        self.args.event = "start"
        with patch.object(META, "inspect", side_effect=[container, image]) as inspect:
            META.record(self.args)
        self.assertEqual(inspect.call_args_list[1].args, ("image", "sha256:actual"))
        manifest = self.manifest()
        self.assertEqual(manifest["created"], previous["created"])
        self.assertEqual(manifest["extra"], {"keep": True})
        self.assertEqual(manifest["containerId"], "container-actual")
        self.assertEqual(manifest["image"]["id"], "sha256:actual")
        self.assertEqual(manifest["image"]["reference"], "fixture:pinned")
        self.assertEqual(manifest["image"]["recordingFormat"], "3")
        self.assertEqual(manifest["image"]["extraImageField"], "keep")
        self.assertEqual(atuin.read_bytes(), b"unchanged history fixture")
        self.args.event = "remove"
        with patch.object(META, "inspect", side_effect=AssertionError("remove must work offline")):
            META.record(self.args)
        self.assertEqual(self.manifest()["containerId"], "container-actual")
        self.assertEqual([event["event"] for event in self.events()], ["init", "start", "remove"])

    def test_failed_inspection_does_not_write_success_or_registry(self):
        self.args.event = "create"
        with patch.object(META, "inspect", side_effect=RuntimeError("not running")):
            with self.assertRaises(RuntimeError):
                META.record(self.args)
        self.assertFalse(self.loot.exists())
        self.assertFalse((self.config/"pomdock/engagement-roots.json").exists())

    def test_registry_conflict_and_corrupt_manifest_are_not_overwritten(self):
        META.record(self.args)
        before = self.manifest()
        self.args.loot = str(self.root/"other-loot")
        with self.assertRaises(ValueError):
            META.record(self.args)
        self.assertEqual(self.manifest(), before)
        self.assertFalse(Path(self.args.loot).exists())
        self.args.loot = str(self.loot)
        path = self.loot/".pomdock/engagement.json"
        path.write_text("{truncated")
        with self.assertRaises(ValueError):
            META.record(self.args)
        self.assertEqual(path.read_text(), "{truncated")

    def test_default_name_and_macos_registry_location(self):
        self.args.name = self.args.container = "pcm-pentest"
        with patch.object(META.sys, "platform", "darwin"):
            META.record(self.args)
        roots = json.loads((self.root/"home/Library/Application Support/pomdock/engagement-roots.json").read_text())
        self.assertEqual(roots, {"(default)": str(self.loot)})
        self.assertEqual(self.manifest()["name"], "(default)")


if __name__ == "__main__":
    unittest.main()
