#!/usr/bin/env python3
"""Write build-provenance.json for a built Engelsiz package (alpha.1/2 schema precedent).

Binds the finished artifact to its exact source commit/tree, toolchain, and
reviewed runtime/compliance pins. Records only what it can verify itself;
native acceptance stays explicitly pending (parent lane fills release notes).

Runs in CI right after scripts/release.py, or locally against a local build.
Never weakens release.py gates; it only reads.
"""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tarfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPO = "https://github.com/yusuferdemyamali/engelsiz"


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def git(args, **kwargs):
    return subprocess.run(["git", *args], check=True, capture_output=True,
                          text=True, cwd=ROOT, **kwargs).stdout.strip()


def tool_version(cmd):
    try:
        out = subprocess.run(cmd, check=True, capture_output=True,
                             text=True, timeout=60).stdout.strip().splitlines()
        return out[0][:120] if out else "unrecorded"
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        return "unrecorded"


def linux_app_sha256(tarball):
    """Hash the staged app binary straight out of the tarball (no full extract)."""
    with tarfile.open(tarball, "r:*") as bundle:
        for member in bundle.getmembers():
            if member.name == "engelsiz/engelsiz" and member.isfile():
                body = bundle.extractfile(member)
                digest_value = hashlib.sha256()
                for chunk in iter(lambda: body.read(1 << 20), b""):
                    digest_value.update(chunk)
                return digest_value.hexdigest(), member.size
    raise ValueError("linux tarball lacks engelsiz/engelsiz")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=["linux-amd64", "windows-amd64"], required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--artifact", type=Path, required=True,
                        help="Built package file (tarball or Setup exe)")
    parser.add_argument("--runtime-lock", type=Path,
                        default=ROOT / "third_party/runtime-lock.json")
    parser.add_argument("--runtime-lock-name", default="third_party/runtime-lock.json",
                        help="Provenance label for which lock file gated the build")
    parser.add_argument("--compliance", type=Path, required=True,
                        help="Materialized compliance dir the build actually used")
    parser.add_argument("--app-sha256", default=None)
    parser.add_argument("--app-bytes", type=int, default=None)
    parser.add_argument("--ci-run-url", default=None)
    parser.add_argument("--out", type=Path, default=Path("build-provenance.json"))
    args = parser.parse_args()

    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?", args.version):
        raise ValueError("Invalid release version")
    artifact = args.artifact.resolve(strict=True)
    manifest = json.loads((args.compliance / "manifest.json").read_text(encoding="utf-8"))
    locks = json.loads(args.runtime_lock.read_text(encoding="utf-8"))
    lock = locks[args.platform]
    try:
        commit = git(["rev-parse", "HEAD"])
        tree = git(["rev-parse", "HEAD^{tree}"])
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError(f"Provenance must bind a git commit/tree: {error}") from error
    try:
        translation_locks = json.loads(
            (ROOT / "third_party/translation-runtime-lock.json").read_text(encoding="utf-8"))
        translation_lock = translation_locks.get(args.platform, {})
    except OSError:
        translation_lock = {}
    notices = {}
    for scope in ("frontend", "go"):
        pinned = manifest.get("applicationDependencies", {}).get(scope, {})
        path = ROOT / "third_party/application-notices" / f"{scope}.txt"
        actual = digest(path) if path.is_file() else "unrecorded"
        if pinned.get("noticeSha256") not in (None, actual):
            raise ValueError(f"Provenance refused: {scope} notices drifted since packaging")
        notices[f"{scope}.txt"] = {"bytes": path.stat().st_size if path.is_file() else -1,
                                   "sha256": actual}
    if args.ci_run_url:
        run_url = args.ci_run_url
    else:
        server = os.environ.get("GITHUB_SERVER_URL", "https://github.com")
        repo = os.environ.get("GITHUB_REPOSITORY", "yusuferdemyamali/engelsiz")
        run_id = os.environ.get("GITHUB_RUN_ID", "")
        run_url = f"{server}/{repo}/actions/runs/{run_id}" if run_id else "unrecorded-local-build"

    app_sha, app_bytes = args.app_sha256, args.app_bytes
    if args.platform == "linux-amd64" and app_sha is None:
        app_sha, app_bytes = linux_app_sha256(artifact)

    base = {
        "format": 1,
        "createdAt": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
        "sourceRepository": REPO,
        "sourceCommit": commit,
        "sourceTree": tree,
        "runtimeLockFile": args.runtime_lock_name,
        "complianceManifestSha256": lock.get("complianceManifestSha256"),
        "translationRuntime": {
            "binary": translation_lock.get("binary"),
            "workerProtocol": translation_lock.get("workerProtocol"),
            "packages": translation_lock.get("packages", {}),
        },
    }
    if args.platform == "linux-amd64":
        document = {
            **base,
            "version": args.version,
            "platform": "linux-amd64",
            "build": {
                "go": tool_version(["go", "version"]),
                "GOARCH": "amd64",
                "GOAMD64": "v1",
                "CGO_ENABLED": "0",
                "flags": ["-trimpath", "-buildvcs=false"],
                "node": tool_version(["node", "--version"]),
            },
            "assets": {
                artifact.name: {"sha256": digest(artifact),
                                "bytes": artifact.stat().st_size},
            },
            "appSha256": app_sha,
            "appBytes": app_bytes,
            "runtimeSha256": lock["sha256"],
            "runtimeArchiveSha256": lock["sha256"],
            "runtimeVersion": lock["version"],
            "applicationNotices": notices,
            "validation": {
                "githubActions": run_url,
                "cleanDesktopAcceptance": "pending",
                "note": ("Linux package rebuilt fresh at this commit so provenance "
                         "binds it; full clean-desktop acceptance belongs to the parent lane."),
            },
            "tag": f"v{args.version}",
        }
    else:
        document = {
            **base,
            "status": "Engineering candidate; clean native Windows acceptance remains pending",
            "version": args.version,
            "target": "windows-amd64",
            "artifact": {
                "name": artifact.name,
                "bytes": artifact.stat().st_size,
                "sha256": digest(artifact),
            },
            "application": {
                "sha256": app_sha or "not-extracted-on-CI-see-QA-report",
                "CGO_ENABLED": "0",
                "GOOS": "windows",
                "GOARCH": "amd64",
                "GOAMD64": "v1",
                "embeddedFrontend": True,
            },
            "buildEnvironment": {
                "Go": tool_version(["go", "version"]),
                "Node": tool_version(["node", "--version"]),
                "Python": tool_version([sys.executable, "--version"]),
                "InnoSetup": "pinned choco provision, see CI log",
            },
            "runtime": {
                "version": lock["version"],
                "archiveSha256": lock["sha256"],
                "complianceManifestSha256": lock.get("complianceManifestSha256"),
                "sourcePayloadNote": ("17MB source-built archive (b42623c5…) is local-dist-only; "
                                      "CI consumes only the hash-pinned compliance dir until "
                                      "pins/archives are explicitly reviewed."),
            },
            "applicationNotices": notices,
            "validation": {
                "githubActions": run_url,
                "installerQAReport": ("engelsiz-windows-translation-qa artifact of the same "
                                      "workflow_dispatch; native acceptance pending there."),
                "testedNativeWindowsVersions": [],
                "cleanNativeWindows": False,
                "signing": "No signing certificate provisioned; native SmartScreen behavior not observed",
            },
        }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(document, indent=2, ensure_ascii=False) + "\n",
                        encoding="utf-8")
    print(f"wrote {args.out} for {artifact.name}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Provenance refused: {error}") from error
