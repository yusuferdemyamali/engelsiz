"""Focused provenance tests: selected candidate lock drives runtime fields.

Covers the bounded release-correctness fix: the CI provenance step must pass
--runtime-lock/--runtime-lock-name for the platform's candidate lock (the same
path the package step feeds to release.py), so published runtime hashes bind
the actual build instead of the default official-archive lock.
"""
import hashlib
import importlib.util
import io
import json
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "write_build_provenance",
    Path(__file__).parents[1] / "write-build-provenance.py",
)
provenance = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(provenance)

WORKFLOW = Path(__file__).parents[2] / ".github/workflows/release.yml"

LINUX_LOCK = "third_party/runtime-source-candidates/offline-maps/linux-amd64/candidate-runtime-lock.json"
WINDOWS_LOCK = "third_party/runtime-source-candidates/offline-maps/windows-amd64/candidate-runtime-lock.json"


class SelectedLockTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        # Distinct per-platform pins prove the selected lock (not a default)
        # drives the emitted runtime fields.
        self.locks = {
            "linux-amd64": {
                "sha256": "a" * 64,
                "version": "candidate-linux-1",
                "complianceManifestSha256": "c" * 64,
            },
            "windows-amd64": {
                "sha256": "b" * 64,
                "version": "candidate-windows-2",
                "complianceManifestSha256": "d" * 64,
            },
        }
        self.lock_path = self.root / "candidate-runtime-lock.json"
        self.lock_path.write_text(json.dumps(self.locks), encoding="utf-8")
        notices_dir = self.root / "third_party/application-notices"
        notices_dir.mkdir(parents=True, exist_ok=True)
        self.notice_digests = {}
        for scope in ("frontend", "go"):
            body = f"synthetic {scope} notices\n".encode()
            (notices_dir / f"{scope}.txt").write_bytes(body)
            self.notice_digests[scope] = hashlib.sha256(body).hexdigest()
        self.compliance = self.root / "compliance"
        self.compliance.mkdir(parents=True, exist_ok=True)
        manifest = {
            "applicationDependencies": {
                scope: {"noticeSha256": self.notice_digests[scope]}
                for scope in ("frontend", "go")
            }
        }
        (self.compliance / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        (self.root / "third_party/translation-runtime-lock.json").write_text(
            json.dumps({"linux-amd64": {"binary": "t", "workerProtocol": "p", "packages": {}},
                        "windows-amd64": {"binary": "t", "workerProtocol": "p", "packages": {}}}),
            encoding="utf-8",
        )

    def _run(self, platform, artifact, extra_args=(), version="0.1.0-alpha.5"):
        out = self.root / f"provenance-{platform}.json"
        argv = ["write-build-provenance.py", "--platform", platform,
                "--version", version, "--artifact", str(artifact),
                "--runtime-lock", str(self.lock_path),
                "--runtime-lock-name", "selected/candidate-runtime-lock.json",
                "--compliance", str(self.compliance), "--out", str(out),
                *extra_args]

        def fake_git(args, **kwargs):
            if args[:2] == ["rev-parse", "HEAD"]:
                return "f" * 40
            return "e" * 40

        with (patch.object(provenance, "ROOT", self.root),
              patch.object(provenance, "git", side_effect=fake_git),
              patch.object(provenance, "tool_version", return_value="test-tool"),
              patch.object(sys, "argv", argv)):
            provenance.main()
        return json.loads(out.read_text(encoding="utf-8"))

    def _linux_tarball(self):
        path = self.root / "engelsiz-linux-x64.tar.gz"
        body = b"fake-linux-app-binary"
        with tarfile.open(path, "w:gz") as bundle:
            member = tarfile.TarInfo("engelsiz/engelsiz")
            member.size = len(body)
            bundle.addfile(member, io.BytesIO(body))
        return path, body

    def test_linux_selected_lock_drives_runtime_fields(self):
        tarball, body = self._linux_tarball()
        doc = self._run("linux-amd64", tarball)
        lock = self.locks["linux-amd64"]
        self.assertEqual(doc["runtimeSha256"], lock["sha256"])
        self.assertEqual(doc["runtimeArchiveSha256"], lock["sha256"])
        self.assertEqual(doc["complianceManifestSha256"], lock["complianceManifestSha256"])
        self.assertEqual(doc["runtimeLockFile"], "selected/candidate-runtime-lock.json")
        self.assertEqual(doc["runtimeVersion"], lock["version"])
        self.assertEqual(doc["sourceCommit"], "f" * 40)
        self.assertEqual(doc["sourceTree"], "e" * 40)
        self.assertEqual(doc["version"], "0.1.0-alpha.5")
        self.assertEqual(doc["appSha256"], hashlib.sha256(body).hexdigest())
        self.assertEqual(doc["appBytes"], len(body))
        self.assertEqual(doc["assets"][tarball.name]["sha256"],
                         hashlib.sha256(tarball.read_bytes()).hexdigest())

    def test_windows_selected_lock_drives_runtime_fields(self):
        exe = self.root / "Engelsiz-Setup.exe"
        exe.write_bytes(b"synthetic-windows-setup")
        doc = self._run("windows-amd64", exe)
        lock = self.locks["windows-amd64"]
        self.assertEqual(doc["runtime"]["archiveSha256"], lock["sha256"])
        self.assertEqual(doc["runtime"]["version"], lock["version"])
        self.assertEqual(doc["runtime"]["complianceManifestSha256"],
                         lock["complianceManifestSha256"])
        self.assertEqual(doc["runtimeLockFile"], "selected/candidate-runtime-lock.json")
        self.assertEqual(doc["sourceCommit"], "f" * 40)
        self.assertEqual(doc["version"], "0.1.0-alpha.5")
        self.assertEqual(doc["artifact"]["sha256"],
                         hashlib.sha256(exe.read_bytes()).hexdigest())
        note = doc["runtime"]["sourcePayloadNote"]
        self.assertIn("--runtime-lock", note)
        self.assertNotIn("local-dist-only", note)
        self.assertNotIn("CI consumes only", note)

    def test_workflow_provenance_wires_selected_candidate_lock(self):
        text = WORKFLOW.read_text(encoding="utf-8")
        section = text.split("Write build provenance", 1)[1].split("Upload build provenance")[0]
        self.assertIn("--runtime-lock", section)
        self.assertIn("--runtime-lock-name", section)
        self.assertIn(LINUX_LOCK, section)
        self.assertIn(WINDOWS_LOCK, section)
        # Per-platform selection must survive (matrix lookup, not a fixed lock).
        self.assertIn("matrix.platform", section)

    def test_release_channel_stable_both_platforms(self):
        tarball, _ = self._linux_tarball()
        linux_doc = self._run("linux-amd64", tarball, version="1.0.0")
        exe = self.root / "Engelsiz-Setup.exe"
        exe.write_bytes(b"synthetic-windows-setup")
        windows_doc = self._run("windows-amd64", exe, version="1.0.0")
        for doc in (linux_doc, windows_doc):
            self.assertEqual(doc["releaseChannel"], "stable")
            self.assertEqual(doc["version"], "1.0.0")
        self.assertEqual(linux_doc["tag"], "v1.0.0")
        # Honest pending caveats stay; only the product-channel wording is gone.
        self.assertNotIn("Engineering candidate", json.dumps(windows_doc))
        self.assertIn("pending", windows_doc["status"])
        self.assertFalse(windows_doc["validation"]["cleanNativeWindows"])
        self.assertEqual(windows_doc["validation"]["testedNativeWindowsVersions"], [])
        self.assertIn("No signing certificate provisioned",
                      windows_doc["validation"]["signing"])
        self.assertEqual(windows_doc["application"]["sha256"],
                         "not-extracted-on-CI-see-QA-report")

    def test_release_channel_prerelease_both_platforms(self):
        tarball, _ = self._linux_tarball()
        linux_doc = self._run("linux-amd64", tarball, version="1.0.0-rc.1")
        exe = self.root / "Engelsiz-Setup.exe"
        exe.write_bytes(b"synthetic-windows-setup")
        windows_doc = self._run("windows-amd64", exe, version="1.0.0-rc.1")
        for doc in (linux_doc, windows_doc):
            self.assertEqual(doc["releaseChannel"], "prerelease")
            self.assertEqual(doc["version"], "1.0.0-rc.1")
        self.assertEqual(linux_doc["tag"], "v1.0.0-rc.1")
        self.assertNotIn("Engineering candidate", json.dumps(windows_doc))
        self.assertFalse(windows_doc["validation"]["cleanNativeWindows"])


if __name__ == "__main__":
    unittest.main()
